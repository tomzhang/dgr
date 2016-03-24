#!/dgr/bin/busybox sh
set -e
. /dgr/bin/functions.sh
isLevelEnabled "debug" && set -x

rm -Rf /bin
rm -Rf /usr
ln -s /dgr/bin /bin
ln -s /dgr/usr /usr


keyringFile=debian-archive-keyring_2014.3_all.deb
cdebootstrapFile=cdebootstrap-static_0.7.3_amd64.deb
gpgvFile=gpgv_1.4.18-7_amd64.deb


mkdir /tmp/debootstrap
cd /tmp/debootstrap
wget http://ftp.us.debian.org/debian/pool/main/c/cdebootstrap/${cdebootstrapFile}
ar -x ${cdebootstrapFile}
cd /
zcat /tmp/debootstrap/data.tar.xz | tar xv

mkdir /tmp/keyring
cd /tmp/keyring
wget http://ftp.us.debian.org/debian/pool/main/d/debian-archive-keyring/${keyringFile}
ar -x ${keyringFile}
cd /
zcat /tmp/keyring/data.tar.xz | tar xv

mkdir /tmp/gpgv
cd /tmp/gpgv
wget http://ftp.us.debian.org/debian/pool/main/g/gnupg/${gpgvFile}
ar -x ${gpgvFile}
cd /
zcat /tmp/gpgv/data.tar.xz | tar xv

echo 'Debootstrapping new Jessie image'
LANG=C /usr/bin/cdebootstrap-static --arch=amd64 --flavour=minimal --verbose jessie ${ROOTFS}

rm -Rf  ${ROOTFS}/usr/share/locale/*