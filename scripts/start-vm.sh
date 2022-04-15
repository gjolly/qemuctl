#!/bin/bash -eu

image="${1}"
format="${FORMAT:-$(qemu-img info --output=json "$image" | jq -r .format)}"

if [ -z "$image" ]; then
  echo "usage: $0 DISK_IMAGE"
  exit 1
fi

if [ -z "$format" ]; then
  echo "Cannot detect image format, please set FORMAT"
  exit 1
fi

# what's this??
tpm_cancel="/tmp/cancel-foo"
touch "$tpm_cancel"

# To be able to modify the boot order
cp /usr/share/OVMF/OVMF_VARS_4M.ms.fd /tmp/

user_data="/tmp/user-data.yaml"
cat << EOF > "$user_data"
#cloud-config
ssh_import_id: [gjolly]
EOF

seed_img='/tmp/seed.img'
cloud-localds "$seed_img" "$user_data"

clean() {
  rm -f /tmp/OVMF_VARS_4M.ms.fd "$tpm_cancel" "$user_data" "$seed_img"
}

trap clean EXIT

###################### setup swtpm #############################

mkdir -p /var/tmp/mytpm1
swtpm socket --tpmstate dir=/var/tmp/mytpm1 \
    --ctrl type=unixio,path=/var/tmp/mytpm1/swtpm-sock \
    --log level=40 --tpm2 -t -d

################################################################


params="-cpu host -machine type=q35,accel=kvm -m 2048"
params="$params -nographic -snapshot"
params="$params -netdev id=net00,type=user,hostfwd=tcp::2222-:22"
params="$params -device virtio-net-pci,netdev=net00"
params="$params -drive if=virtio,format=$format,file=$image"
params="$params -drive if=virtio,format=raw,file=$seed_img"
params="$params -drive if=pflash,format=raw,file=/usr/share/OVMF/OVMF_CODE_4M.secboot.fd,readonly=true"
params="$params -drive if=pflash,format=raw,file=/tmp/OVMF_VARS_4M.ms.fd"
params="$params -chardev socket,id=chrtpm,path=/var/tmp/mytpm1/swtpm-sock"
params="$params -tpmdev emulator,id=tpm0,chardev=chrtpm"
params="$params -device tpm-tis,tpmdev=tpm0"

set -x
qemu-system-x86_64 $params
