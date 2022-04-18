package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"syscall"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
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
	"aarch64": {
		CodePath: "/usr/share/qemu-efi-aarch64/QEMU_EFI.fd",
		VarsPath: "/usr/share/qemu-efi-aarch64/QEMU_EFI.fd",
	},
}

func main() {
	imagePath := flag.String("image", "", "Image containing the root filesystem to boot.")
	suite := flag.String("suite", "", "Download the latest image to this suite and start it.")
	uefi := flag.Bool("uefi", false, "Use UEFI firemware.")
	arch := flag.String("arch", "x86_64", "Architecture to use")
	memory := flag.Int("memory", 2048, "Memory to allocate")
	noSnapshot := flag.Bool("no-snapshot", false, "Automatically commit changes to the image")
	sshKey := flag.String("sshkey", "", "Public key to add to the user.")
	sshID := flag.String("sshid", "", "ID used to import a key using ssh-import-id.")
	uefiVars := flag.String("uefivars", "", "File containing UEFI variable (eg with default boot entry set).")
	flag.Parse()

	tmpDir, err := os.MkdirTemp(os.TempDir(), "qemu")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)

	if *imagePath == "" {
		if *suite == "" || *arch == "" {
			fmt.Println("no image specified or suite and arch specified")
			flag.Usage()
			os.Exit(1)
		}

		image, err := downloadImage(tmpDir, *suite, *arch)
		if err != nil {
			fmt.Printf("failed to download image for %v %v", *suite, *arch)
			flag.Usage()
			os.Exit(1)
		}

		*imagePath = image
	}

	cloudInitConfig := CloudInitConfig{
		SSHKey:      *sshKey,
		SSHImportID: []string{*sshID},
	}
	cloudInitSeedPath, err := generateCloudInitSeed(tmpDir, &cloudInitConfig)

	if err != nil {
		panic(err)
	}
	params := QemuParams{
		ImagePath:          *imagePath,
		CloudInitSeedPath:  cloudInitSeedPath,
		Arch:               *arch,
		Memory:             *memory,
		NoSnapshot:         *noSnapshot,
		UEFIEnabled:        *uefi,
		CustomUEFIVarsFile: *uefiVars,
	}

	err = runQemu(&params, tmpDir)
	if err != nil {
		panic(err)
	}
}

var suiteToVersion = map[string]string{
	"bionic": "18.04",
	"focal":  "20.04",
	"impish": "21.10",
	"jammy":  "22.04",
}

var ubuntuArches = map[string]string{
	"aarch64": "arm64",
	"x86_64":  "amd64",
}

func downloadImage(tmpDir, suite, arch string) (string, error) {
	version := suiteToVersion[suite]
	ubuntuArch := ubuntuArches[arch]

	fileName := fmt.Sprintf("ubuntu-%v-server-cloudimg-%v.img", version, ubuntuArch)
	url := fmt.Sprintf("http://cloud-images.ubuntu.com/releases/%v/release/%v", suite, fileName)

	fmt.Printf("Downloading %v...\n", fileName)

	// Get the data
	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != 200 {
		return "", errors.Wrap(err, "failed to donwload file")
	}
	defer resp.Body.Close()

	// Create the file
	filePath := path.Join(tmpDir, fileName)
	out, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return filePath, err
}

// CloudInitConfig defines how cloud-init will be run
// on the host
type CloudInitConfig struct {
	SSHImportID []string
	SSHKey      string
}

func generateCloudInitSeed(dir string, config *CloudInitConfig) (string, error) {
	userDataPath := path.Join(dir, "user-date.yaml")
	file, err := os.OpenFile(userDataPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, err = file.WriteString("#cloud-config\n")

	if len(config.SSHImportID) != 0 {
		out, _ := yaml.Marshal(map[string][]string{
			"ssh_import_id": config.SSHImportID,
		})
		_, err = file.Write(out)
	}

	if len(config.SSHKey) != 0 {
		out, _ := yaml.Marshal(map[string][]string{
			"ssh_authorized_keys": {config.SSHKey},
		})
		_, err = file.Write(out)
	}

	if err != nil {
		return "", err
	}

	seedPath := path.Join(dir, "seed.img")
	err = runCommand("cloud-localds", seedPath, userDataPath)

	return seedPath, err
}

// QemuParams contains the parameter needed to configure QEMU
type QemuParams struct {
	ImagePath          string
	CloudInitSeedPath  string
	Arch               string
	Memory             int
	UEFIEnabled        bool
	NoSnapshot         bool
	CustomUEFIVarsFile string
}

var defaultOption = map[string][]string{
	"aarch64": {"-cpu", "max", "-machine", "virt"},
	"x86_64":  {"-cpu", "host", "-machine", "q35"},
}

func charToString(bs [65]int8) string {
	b := make([]byte, 65)
	strLen := 0
	for i, v := range bs {
		if v == 0 {
			break
		}
		b[i] = byte(v)
		strLen++
	}
	return string(b[:strLen])
}

func getSystemArch() string {
	utsname := syscall.Utsname{}
	syscall.Uname(&utsname)

	return charToString(utsname.Machine)
}

func kvmSupported(VMArch string) bool {
	arch := getSystemArch()

	if arch != VMArch {
		return false
	}

	if _, err := os.Stat("/sys/module/kvm"); os.IsNotExist(err) {
		return false
	}

	return true
}

func runQemu(qemuParams *QemuParams, tmpDir string) error {
	params := defaultOption[qemuParams.Arch]
	params = append(params, "-m", fmt.Sprintf("%v", qemuParams.Memory), "-nographic")

	if !qemuParams.NoSnapshot {
		params = append(params, "-snapshot")
	}

	if kvmSupported(qemuParams.Arch) {
		params = append(params, "--enable-kvm")
	}

	params = append(params, "-netdev", "id=net00,type=user,hostfwd=tcp::2222-:22",
		"-device", "virtio-net-pci,netdev=net00",
		"-drive", fmt.Sprintf("if=virtio,format=qcow2,file=%v", qemuParams.ImagePath),
		"-drive", fmt.Sprintf("if=virtio,format=raw,file=%v", qemuParams.CloudInitSeedPath),
	)

	if qemuParams.UEFIEnabled {
		uefiFiremwareCodePath, uefiFirmwareVarsPath, _ := getUEFIFirmware(
			tmpDir, qemuParams.CustomUEFIVarsFile, qemuParams.Arch)

		params = append(params,
			"-drive", fmt.Sprintf("if=pflash,format=raw,file=%v,readonly=true", uefiFiremwareCodePath),
			"-drive", fmt.Sprintf("if=pflash,format=raw,file=%v", uefiFirmwareVarsPath),
		)
	}

	err := runCommand(fmt.Sprintf("qemu-system-%v", qemuParams.Arch), params...)
	return err
}

func createEmptyFile(path string, size int) error {
	err := runCommand("dd", "if=/dev/zero",
		fmt.Sprintf("of=%v", path),
		"bs=1M", fmt.Sprintf("count=%v", size),
	)

	return err
}

func getUEFIFirmware(tmpDir, uefiVarsFile, arch string) (string, string, error) {
	uefiFiremwareCodePath := UEFIFirmwares[arch].CodePath
	uefiFirmwareVarsPath := UEFIFirmwares[arch].VarsPath
	if uefiVarsFile != "" {
		uefiFirmwareVarsPath = uefiVarsFile
	}

	newUEFIFirmwareVarsPath := path.Join(tmpDir, "UEFI_VARS.img")
	if arch == "aarch64" {
		err := createEmptyFile(newUEFIFirmwareVarsPath, 64)
		if err != nil {
			return "", "", err
		}
		uefiFirmwareVarsPath = newUEFIFirmwareVarsPath

		newUEFIFirmwareCodePath := path.Join(tmpDir, "UEFI_CODE.img")
		err = createEmptyFile(newUEFIFirmwareCodePath, 64)
		if err != nil {
			return "", "", err
		}

		err = runCommand("dd",
			fmt.Sprintf("if=%v", uefiFiremwareCodePath),
			fmt.Sprintf("of=%v", newUEFIFirmwareCodePath),
			"conv=notrunc",
		)
		if err != nil {
			return "", "", err
		}

		uefiFiremwareCodePath = newUEFIFirmwareCodePath
	} else if arch == "x86_64" {
		// This is to allow modifications of variables
		copyFile(uefiFirmwareVarsPath, newUEFIFirmwareVarsPath)
		uefiFirmwareVarsPath = newUEFIFirmwareVarsPath
	}

	return uefiFiremwareCodePath, uefiFirmwareVarsPath, nil
}

func copyFile(src, dst string) error {
	fin, err := os.Open(src)
	if err != nil {
		return err
	}
	defer fin.Close()

	fout, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer fout.Close()

	_, err = io.Copy(fout, fin)

	return err
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)

	env := os.Environ()
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	fmt.Printf(">>> %v\n", cmd.String())
	err := cmd.Start()
	if err != nil {
		return err
	}

	err = cmd.Wait()
	return err
}
