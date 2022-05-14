# qemuctl

Go tool to run qemu with basic defaults.

## Usage

To start Ubuntu Focal:

```
./qemuctl -suite focal -arch x86_64 -uefi -sshkey "$(cat PATH_TO_KEY)"
```

To start a specific image:

```
./qemuctl -image IMG_PATH -uefi -sshkey "$(cat PATH_TO_KEY)"
```
