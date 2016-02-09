package builder

import (
	"github.com/appc/spec/schema"
	"github.com/blablacar/cnt/cnt"
	"github.com/blablacar/cnt/spec"
	"github.com/blablacar/cnt/utils"
	"github.com/ghodss/yaml"
	"github.com/n0rad/go-erlog/data"
	"github.com/n0rad/go-erlog/errs"
	"github.com/n0rad/go-erlog/logs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
)

const SH_FUNCTIONS = `
execute_files() {
  fdir=$1
  [ -d "$fdir" ] || return 0

  for file in $fdir/*; do
    [ -e "$file" ] && {
     	[ -x "$file" ] || /cnt/bin/busybox chmod +x "$file"
		isLevelEnabled 4 && echo -e "\e[1m\e[32mRunning script -> $file\e[0m"
     	$file
    }
  done
}

case ` + "`echo ${LOG_LEVEL:-INFO} | awk '{print toupper($0)}'`" + ` in
	"FATAL") lvl=0 ;;
	"PANIC") lvl=1 ;;
	"ERROR") lvl=2 ;;
	"WARN"|"WARNING") lvl=3 ;;
	"INFO") lvl=4 ;;
	"DEBUG") lvl=5 ;;
	"TRACE") lvl=6 ;;
	*) echo "UNKNOWN LOG LEVEL"; lvl=4 ;;
esac


isLevelEnabled() {
	if [ $1 -le $lvl ]; then
		return 0
	fi
	return 1
}
`

const BUILD_SCRIPT = `#!/cnt/bin/busybox sh
set -e
` + SH_FUNCTIONS + `

isLevelEnabled 5 && set -x

export TARGET=$( dirname $0 )
export ROOTFS=%%ROOTFS%%
export TERM=xterm

execute_files "$ROOTFS/cnt/runlevels/inherit-build-early"
execute_files "$TARGET/runlevels/build"
`

const BUILD_SCRIPT_LATE = `#!/cnt/bin/busybox sh
set -e
` + SH_FUNCTIONS + `

isLevelEnabled 5 && set -x


export TARGET=$( dirname $0 )
export ROOTFS=%%ROOTFS%%
export TERM=xterm

execute_files "$TARGET/runlevels/build-late"
execute_files "$ROOTFS/cnt/runlevels/inherit-build-late"
`

const PRESTART = `#!/cnt/bin/busybox sh
set -e
` + SH_FUNCTIONS + `

isLevelEnabled 5 && set -x


BASEDIR=${0%/*}
CNT_PATH=/cnt

execute_files ${CNT_PATH}/runlevels/prestart-early

${BASEDIR}/templater -L ${LOG_LEVEL} -t / /cnt

#if [ -d ${CNT_PATH}/attributes ]; then
#	echo "$CONFD_OVERRIDE"
#    ${BASEDIR}/attributes-merger -i ${CNT_PATH}/attributes -e CONFD_OVERRIDE
#    export CONFD_DATA=$(cat attributes.json)
#fi
#${BASEDIR}/confd -onetime -config-file=${CNT_PATH}/prestart/confd.toml

execute_files ${CNT_PATH}/runlevels/prestart-late
`
const PATH_BIN = "/bin"
const PATH_TESTS = "/tests"
const PATH_INSTALLED = "/installed"
const PATH_MANIFEST = "/manifest"
const PATH_IMAGE_ACI = "/image.aci"
const PATH_IMAGE_ACI_ZIP = "/image-zip.aci"
const PATH_ROOTFS = "/rootfs"
const PATH_TARGET = "/target"
const PATH_CNT = "/cnt"
const PATH_CNT_MANIFEST = "/cnt-manifest.yml"
const PATH_RUNLEVELS = "/runlevels"
const PATH_PRESTART_EARLY = "/prestart-early"
const PATH_PRESTART_LATE = "/prestart-late"
const PATH_INHERIT_BUILD_LATE = "/inherit-build-late"
const PATH_INHERIT_BUILD_EARLY = "/inherit-build-early"
const PATH_ATTRIBUTES = "/attributes"
const PATH_FILES = "/files"
const PATH_BUILD_LATE = "/build-late"
const PATH_BUILD_SETUP = "/build-setup"
const PATH_BUILD = "/build"
const PATH_TEMPLATES = "/templates"

type Aci struct {
	fields          data.Fields
	path            string
	target          string
	rootfs          string
	podName         *spec.ACFullname
	manifest        spec.AciManifest
	args            BuildArgs
	FullyResolveDep bool
}

func NewAciWithManifest(path string, args BuildArgs, manifest spec.AciManifest, latestChecked *chan bool, compatChecked *chan bool) (*Aci, error) {
	if manifest.NameAndVersion == "" {
		logs.WithField("path", path).Fatal("name is mandatory in manifest")
	}

	fields := data.WithField("aci", manifest.NameAndVersion.String())
	logs.WithF(fields).WithFields(data.Fields{"args": args, "path": path, "manifest": manifest}).Debug("New aci")

	fullPath, err := filepath.Abs(path)
	if err != nil {
		return nil, errs.WithEF(err, fields, "Cannot get fullpath of project")
	}

	target := fullPath + PATH_TARGET
	if cnt.Home.Config.TargetWorkDir != "" {
		currentAbsDir, err := filepath.Abs(cnt.Home.Config.TargetWorkDir + "/" + manifest.NameAndVersion.ShortName())
		if err != nil {
			return nil, errs.WithEF(err, fields.WithField("path", path), "Invalid target path")
		}
		target = currentAbsDir
	}

	aci := &Aci{
		fields:          fields,
		args:            args,
		path:            fullPath,
		manifest:        manifest,
		target:          target,
		rootfs:          target + PATH_ROOTFS,
		FullyResolveDep: true,
	}

	go aci.checkCompatibilityVersions(compatChecked)
	go aci.checkLatestVersions(latestChecked)
	return aci, nil
}

func NewAci(path string, args BuildArgs) (*Aci, error) {
	manifest, err := readAciManifest(path + PATH_CNT_MANIFEST)
	if err != nil {
		return nil, errs.WithEF(err, data.WithField("path", path+PATH_CNT_MANIFEST), "Cannot read manifest")
	}
	return NewAciWithManifest(path, args, *manifest, nil, nil)
}

//////////////////////////////////////////////////////////////////

func readAciManifest(manifestPath string) (*spec.AciManifest, error) {
	manifest := spec.AciManifest{Aci: spec.AciDefinition{}}

	source, err := ioutil.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal([]byte(source), &manifest)
	if err != nil {
		return nil, errs.WithE(err, "Cannot unmarshall manifest")
	}

	return &manifest, nil
}

func (aci *Aci) tarAci(zip bool) {
	target := PATH_IMAGE_ACI[1:]
	if zip {
		target = PATH_IMAGE_ACI_ZIP[1:]
	}
	dir, _ := os.Getwd()
	logs.WithField("path", aci.target).Debug("chdir")
	os.Chdir(aci.target)
	utils.Tar(zip, target, PATH_MANIFEST[1:], PATH_ROOTFS[1:])
	logs.WithField("path", dir).Debug("chdir")
	os.Chdir(dir)
}

func (aci *Aci) checkCompatibilityVersions(compatChecked *chan bool) {
	froms, err := aci.manifest.GetFroms()
	if err != nil {
		logs.WithEF(err, aci.fields).Fatal("Invalid from")
	}
	for _, from := range froms {
		if from == "" {
			continue
		}

		fromFields := aci.fields.WithField("dependency", from.String())

		out, err := utils.ExecCmdGetOutput("rkt", "image", "cat-manifest", from.String())
		if err != nil {
			logs.WithEF(err, fromFields).Fatal("Cannot find dependency")
		}

		version, ok := loadManifest(out).Annotations.Get("cnt-version")
		var val int
		if ok {
			val, err = strconv.Atoi(version)
			if err != nil {
				logs.WithEF(err, fromFields).WithField("version", version).Fatal("Failed to parse cnt-version from manifest")
			}
		}
		if !ok || val < 51 {
			logs.WithF(aci.fields).
				WithField("from", from).
				WithField("require", ">=51").
				Error("from aci was not build with a compatible version of cnt")
		}
	}

	for _, dep := range aci.manifest.Aci.Dependencies {
		out, err := utils.ExecCmdGetOutput("rkt", "image", "cat-manifest", dep.String())
		depFields := aci.fields.WithField("dependency", dep.String())
		if err != nil {
			logs.WithEF(err, depFields).Fatal("Cannot find dependency")
		}

		version, ok := loadManifest(out).Annotations.Get("cnt-version")
		var val int
		if ok {
			val, err = strconv.Atoi(version)
			if err != nil {
				logs.WithEF(err, depFields).WithField("version", version).Fatal("Failed to parse cnt-version from manifest")
			}
		}
		if !ok || val < 51 {
			logs.WithF(aci.fields).
				WithField("dependency", dep).
				WithField("require", ">=51").
				Error("dependency aci was not build with a compatible version of cnt")
		}
	}
	if compatChecked != nil {
		*compatChecked <- true
	}
}

func loadManifest(content string) schema.ImageManifest {
	im := schema.ImageManifest{}
	err := im.UnmarshalJSON([]byte(content))
	if err != nil {
		logs.WithE(err).WithField("content", content).Fatal("Failed to read manifest content")
	}
	return im
}

func (aci *Aci) checkLatestVersions(latestChecked *chan bool) {
	froms, err := aci.manifest.GetFroms()
	if err != nil {
		logs.WithEF(err, aci.fields).Fatal("Invalid from")
	}
	for _, from := range froms {
		if from == "" {
			continue
		}

		version, _ := from.LatestVersion()
		logs.WithField("version", from.Name()+":"+version).Debug("Discovered from latest verion")
		if version != "" && utils.Version(from.Version()).LessThan(utils.Version(version)) {
			logs.WithF(aci.fields.WithField("version", from.Name()+":"+version)).Warn("Newer 'from' version")
		}
	}
	for _, dep := range aci.manifest.Aci.Dependencies {
		if dep.Version() == "" {
			continue
		}
		version, _ := dep.LatestVersion()
		if version != "" && utils.Version(dep.Version()).LessThan(utils.Version(version)) {
			logs.WithF(aci.fields.WithField("version", dep.Name()+":"+version)).Warn("Newer 'dependency' version")
		}
	}
	if latestChecked != nil {
		*latestChecked <- true
	}
}
