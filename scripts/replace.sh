#!/bin/bash -ex

binary_prefix=$(date +%s)

LOCAL_HARV="/tmp/harvester-installer.$binary_prefix"

curl -fL https://github.com/bk201/harvester-installer/releases/download/v1.1.2-hotfix2/harvester-installer -o $LOCAL_HARV
chmod +x $LOCAL_HARV

sed -i s/harvester-installer/harv/ /usr/bin/start-installer.sh
rm -f /usr/bin/harv
ln -s $LOCAL_HARV /usr/bin/harv
systemctl restart getty@tty1.service
