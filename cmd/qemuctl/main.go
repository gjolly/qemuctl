package main

import (
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"github.com/gjolly/qemuctl/internal/commands"
	"github.com/gjolly/qemuctl/internal/config"
	"github.com/gjolly/qemuctl/internal/network"
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
	configPath := flag.String("config", "", "Path to config file")
	flag.Parse()

	config, err := config.GetConfig(*configPath)
	if err != nil {
		panic(err)
	}

	err = network.StartNetworks(config.Network)
	if err != nil {
		panic(err)
	}

	for vmName, vmConfig := range config.Machines {
		go startVM(vmName, vmConfig)
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

func startVM(name string, config config.MachineConfig) {
	tmpDir, err := os.MkdirTemp(os.TempDir(), "qemu")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)

	if config.Image == "" {
		if config.Suite == "" || config.Arch == "" {
			fmt.Println("no image specified or suite and arch specified")
			flag.Usage()
			os.Exit(1)
		}

		image, err := downloadImage(tmpDir, config.Suite, config.Arch)
		if err != nil {
			fmt.Printf("failed to download image for %v %v", config.Suite, config.Arch)
			flag.Usage()
			os.Exit(1)
		}

		config.Image = image
	}

	cloudInitConfig := CloudInitConfig{
		SSHKey:      config.Users["default"].SSHKeys,
		SSHImportID: config.Users["default"].SSHImportID,
		Password:    config.Users["default"].Password,
	}

	cloudInitSeedPath, err := generateCloudInitSeed(tmpDir, &cloudInitConfig)
	if err != nil {
		panic(err)
	}

	tapDevice := ""
	if len(config.Network) != 0 {
		tapDevice, err = network.NewTapDevice(config.Network)
		if err != nil {
			panic(err)
		}
	}

	params := QemuParams{
		ImagePath:         config.Image,
		CloudInitSeedPath: cloudInitSeedPath,
		Arch:              config.Arch,
		Memory:            config.Memory,
		NoSnapshot:        !config.Snapshot,
		UEFIEnabled:       config.UEFI,
		TapDevice:         tapDevice,
	}

	err = runQemu(&params, tmpDir)
	if err != nil {
		panic(err)
	}
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
	SSHKey      []string
	Password    string
}

func generateCloudInitSeed(dir string, config *CloudInitConfig) (string, error) {
	userDataPath := path.Join(dir, "user-date.yaml")
	file, err := os.OpenFile(userDataPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, err = file.WriteString("#cloud-config\n")
	if err != nil {
		return "", err
	}

	if len(config.Password) != 0 {
		_, err = file.WriteString(fmt.Sprintf("password: %v\n", config.Password))
		if err != nil {
			return "", err
		}
		_, err = file.WriteString("chpasswd:\n")
		if err != nil {
			return "", err
		}
		_, err = file.WriteString("  expire: false\n")
		if err != nil {
			return "", err
		}
	}

	if len(config.SSHImportID) != 0 {
		out, _ := yaml.Marshal(map[string][]string{
			"ssh_import_id": config.SSHImportID,
		})
		_, err = file.Write(out)
		if err != nil {
			return "", err
		}
	}

	if len(config.SSHKey) != 0 {
		out, _ := yaml.Marshal(map[string][]string{
			"ssh_authorized_keys": config.SSHKey,
		})
		_, err = file.Write(out)
		if err != nil {
			return "", err
		}
	}

	seedPath := path.Join(dir, "seed.img")
	err = commands.Run("cloud-localds", seedPath, userDataPath)

	return seedPath, err
}

// QemuParams contains the parameter needed to configure QEMU
type QemuParams struct {
	ImagePath          string
	CloudInitSeedPath  string
	Arch               string
	Memory             string
	UEFIEnabled        bool
	NoSnapshot         bool
	CustomUEFIVarsFile string
	TapDevice          string
}

var defaultOption = map[string][]string{
	"aarch64": {"-cpu", "max", "-machine", "virt", "-smp", "4"},
	"x86_64":  {"-cpu", "host", "-machine", "q35", "-smp", "4"},
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

func getCustomUEFIVarPath(imagePath string) (string, bool) {
	dir := path.Dir(imagePath)
	uefiVars := path.Join(dir, "EFI_VARS.fd")
	if _, err := os.Stat(uefiVars); err != nil {
		return uefiVars, false
	}

	return uefiVars, true
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

	if len(qemuParams.TapDevice) == 0 {
		params = append(params, "-netdev", "id=net00,type=user,hostfwd=tcp::2222-:22",
			"-device", "virtio-net-pci,netdev=net00",
		)
	} else {
		rand.Seed(time.Now().UnixNano())
		macAddr := fmt.Sprintf("52:54:00:%x:%x:%x", rand.Intn(255), rand.Intn(255), rand.Intn(255))
		params = append(params, "-netdev", fmt.Sprintf("tap,id=net0,ifname=%v,script=no,downscript=no",
			qemuParams.TapDevice))
		params = append(params, "-device", fmt.Sprintf("e1000,netdev=net0,mac=%v", macAddr))
	}

	if qemuParams.UEFIEnabled {
		uefiFiremwareCodePath, uefiFirmwareVarsPath, _ := getUEFIFirmware(
			tmpDir, qemuParams.CustomUEFIVarsFile, qemuParams.Arch)

		if qemuParams.NoSnapshot && qemuParams.CustomUEFIVarsFile == "" {
			customVars, exists := getCustomUEFIVarPath(qemuParams.ImagePath)
			if exists {
				uefiFirmwareVarsPath = customVars
			} else {
				copyFile(uefiFirmwareVarsPath, customVars)
				uefiFirmwareVarsPath = customVars
			}
		}

		params = append(params,
			"-drive", fmt.Sprintf("if=pflash,format=raw,file=%v,readonly=true", uefiFiremwareCodePath),
			"-drive", fmt.Sprintf("if=pflash,format=raw,file=%v", uefiFirmwareVarsPath),
		)
	}

	params = append(params,
		"-drive", fmt.Sprintf("if=virtio,format=qcow2,file=%v", qemuParams.ImagePath),
		"-drive", fmt.Sprintf("if=virtio,format=raw,file=%v", qemuParams.CloudInitSeedPath),
	)
	err := commands.Run(fmt.Sprintf("qemu-system-%v", qemuParams.Arch), params...)
	return err
}

func createEmptyFile(path string, size int) error {
	err := commands.Run("dd", "if=/dev/zero",
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

		err = commands.Run("dd",
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
