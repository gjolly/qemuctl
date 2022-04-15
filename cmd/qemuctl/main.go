package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
)

// UEFIFirmware is defines the path to the UEFI parameters
type UEFIFirmware struct {
	CodePath string
	VarsPath string
}

// UEFIFirmwares list all the firwmares by arch
var UEFIFirmwares = map[string]UEFIFirmware{
	"x86_64": {
		CodePath: "/usr/share/OVMF/OVMF_CODE_4M.secboot.fd",
		VarsPath: "/usr/share/OVMF/OVMF_VARS_4M.ms.fd",
	},
}

func main() {
	imagePath := flag.String("image", "", "Image containing the root filesystem to boot.")
	uefi := flag.Bool("uefi", false, "Use UEFI firemware.")
	arch := flag.String("arch", "x86_64", "Architecture to use")
	flag.Parse()

	if *imagePath == "" {
		fmt.Println("no image specified")
		flag.Usage()
		os.Exit(1)
	}

	tmpDir, err := os.MkdirTemp(os.TempDir(), "qemu")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)

	cloudInitSeedPath, err := generateCloudInitSeed(tmpDir, "gh:gjolly")
	if err != nil {
		panic(err)
	}
	params := QemuParams{
		ImagePath:         *imagePath,
		CloudInitSeedPath: cloudInitSeedPath,
		Arch:              *arch,
	}

	if *uefi {
		params.UEFIFiremwareCodePath = UEFIFirmwares[*arch].CodePath
		params.UEFIFirmwareVarsPath = UEFIFirmwares[*arch].VarsPath
	}

	err = runQemu(&params)
	if err != nil {
		panic(err)
	}
}

func generateCloudInitSeed(dir, sshKeyID string) (string, error) {
	userDataPath := path.Join(dir, "user-date.yaml")
	file, err := os.OpenFile(userDataPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, err = file.WriteString(fmt.Sprintf(`#cloud-config
ssh_import_id: [%s]
`, sshKeyID))

	if err != nil {
		return "", err
	}

	seedPath := path.Join(dir, "seed.img")
	err = runCommand("cloud-localds", seedPath, userDataPath)

	return seedPath, err
}

// QemuParams contains the parameter needed to configure QEMU
type QemuParams struct {
	ImagePath             string
	CloudInitSeedPath     string
	Arch                  string
	UEFIFiremwareCodePath string
	UEFIFirmwareVarsPath  string
}

func runQemu(qemuParams *QemuParams) error {
	params := []string{"-cpu", "host", "-machine", "type=q35,accel=kvm",
		"-m", "2048", "-nographic", "-snapshot", "-netdev",
		"id=net00,type=user,hostfwd=tcp::2222-:22",
		"-device", "virtio-net-pci,netdev=net00",
		"-drive", fmt.Sprintf("if=virtio,format=qcow2,file=%v", qemuParams.ImagePath),
		"-drive", fmt.Sprintf("if=virtio,format=raw,file=%v", qemuParams.CloudInitSeedPath),
	}

	if qemuParams.UEFIFiremwareCodePath != "" {
		params = append(params,
			"-drive", fmt.Sprintf("if=pflash,format=raw,file=%v,readonly=true", qemuParams.UEFIFiremwareCodePath),
			"-drive", fmt.Sprintf("if=pflash,format=raw,file=%v", qemuParams.UEFIFirmwareVarsPath),
		)
	}

	err := runCommand("qemu-system-x86_64", params...)
	return err
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)

	env := os.Environ()
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	err := cmd.Start()
	if err != nil {
		return err
	}

	err = cmd.Wait()
	return err
}
