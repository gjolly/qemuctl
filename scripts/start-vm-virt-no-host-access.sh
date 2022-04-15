#!/bin/bash -eux

image="$1"

if [ -z "$image" ]; then
  echo "Usage: $0 IMAGE"
fi

tmpdir=$(mktemp -d)
user_data="$tmpdir/user-data.yaml"

cat > "$user_data" <<EOF
#cloud-config
ssh_import_id:
  - gh:gjolly
EOF

cloud-localds $tmpdir/seed.img "$user_data"
cp /usr/share/OVMF/OVMF_VARS.fd "$tmpdir"

macaddress=$(./generate-mac.sh)

qemu-system-x86_64 \
  -nographic \
  -cpu host \
  -enable-kvm \
  -smp 4 \
  -m 4G \
  -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE.fd \
  -drive if=pflash,format=raw,file="$tmpdir/OVMF_VARS.fd" \
  -device e1000,netdev=net0,mac=$macaddress -netdev tap,id=net0,script=./qemu-ifup \
  -drive if=virtio,format=qcow2,file="$image" \
  -drive if=virtio,format=raw,file="$tmpdir/seed.img"
