#!/bin/bash -eux

image="$1"

if [ -z "$image" ]; then
  echo "Usage: $0 IMAGE"
fi

user_data="/tmp/user-data.yaml"

cat > "$user_data" <<EOF
#cloud-config
ssh_import_id:
  - gh:gjolly
EOF

cloud-localds /tmp/seed.img "$user_data"
cp /usr/share/OVMF/OVMF_VARS.fd /tmp/

macaddress=$(./generate-mac.sh)

qemu-system-x86_64 \
  -nographic \
  -cpu host \
  -enable-kvm \
  -smp 4 \
  -m 4G \
  -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE.fd \
  -drive if=pflash,format=raw,file=/tmp/OVMF_VARS.fd \
  -device virtio-net-pci,netdev=net0 --netdev user,id=net0,hostfwd=tcp::2222-:22 \
  -device e1000,netdev=net1,mac=$macaddress -netdev tap,id=net1,script=./qemu-ifup \
  -drive if=virtio,format=qcow2,file="$image" \
  -drive if=virtio,format=raw,file=/tmp/seed.img
