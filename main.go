package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Структуры для хранения данных системы
type SystemInfo struct {
	Processor    ProcessorInfo
	Memory       MemoryInfo
	Network      []NetworkInfo
	GPU          GPUInfo
	Storage      []StorageInfo
	SerialNumber string
}

type ProcessorInfo struct {
	Model     string
	Cores     int
	Threads   int
	Frequency string
	Cache     map[string]string
}

type MemoryInfo struct {
	Total string
	Slots []MemorySlot
}

type MemorySlot struct {
	ID           string
	Size         string
	Type         string
	Speed        string
	Manufacturer string
}

type NetworkInfo struct {
	Interface string
	Model     string
	MAC       string
}

type GPUInfo struct {
	Model         string
	Memory        string
	Driver        string
	Vendor        string
	Architecture  string
	Resolution    string
	OpenGLVersion string
}

type StorageInfo struct {
	Type  string // NVMe, SATA, USB, etc.
	Model string
	Size  string
	Label string
}

// Модели для TUI
type model struct {
	state           int // Состояние программы
	sysInfo         SystemInfo
	width           int
	height          int
	textInput       textinput.Model
	spinner         spinner.Model
	viewport        viewport.Model
	err             error
	userSerial      string
	dmidecodeRaw    string
	logFilePath     string
	showOverlay     bool      // Показывать ли наложение
	overlayContent  string    // Содержимое наложения
	videoTestActive bool      // Активен ли видеотест
	videoTestColor  int       // Текущий цвет видеотеста (0-red, 1-green, 2-blue, 3-testbars)
	videoTestStart  time.Time // Время начала видеотеста
	testPassed      bool      // Прошел ли видеотест успешно
	serialMatched   bool      // Совпал ли серийный номер
}

// Состояния программы
const (
	stateInit = iota
	stateShowInfo
	stateVideoTest
	stateAskVideoOk
	stateAskSerial
	stateCheckSerial
	stateSerialSuccess // Новое состояние для успешной проверки серийного номера
	stateSerialError
	stateCreateLogs
	stateDone
)

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Введите серийный номер"
	ti.Focus()
	ti.CharLimit = 30
	ti.Width = 30

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	vp := viewport.New(80, 20)

	return model{
		state:          stateInit,
		textInput:      ti,
		spinner:        s,
		viewport:       vp,
		showOverlay:    false,
		videoTestColor: 0,
	}
}

func (m model) Init() tea.Cmd {
	// Проверяем root и начинаем сбор данных
	return tea.Batch(
		checkRootCmd,
		spinner.Tick,
		collectSystemInfoCmd,
	)
}

// Команда для проверки root прав
func checkRootCmd() tea.Msg {
	if os.Geteuid() != 0 {
		return errMsg{error: fmt.Errorf("эта программа должна быть запущена с правами root")}
	}
	return nil
}

type errMsg struct {
	error
}

// Команды для сбора системной информации
func collectSystemInfoCmd() tea.Msg {
	sysInfo := SystemInfo{}
	var err error

	// Получение информации о процессоре
	sysInfo.Processor, err = getProcessorInfo()
	if err != nil {
		return errMsg{err}
	}

	// Получение информации о памяти
	sysInfo.Memory, err = getMemoryInfo()
	if err != nil {
		return errMsg{err}
	}

	// Получение информации о сетевых картах
	sysInfo.Network, err = getNetworkInfo()
	if err != nil {
		return errMsg{err}
	}

	// Получение информации о GPU
	sysInfo.GPU, err = getGPUInfo()
	if err != nil {
		return errMsg{err}
	}

	// Получение информации о накопителях
	sysInfo.Storage, err = getStorageInfo()
	if err != nil {
		return errMsg{err}
	}

	// Получение серийного номера из dmidecode
	dmidecodeRaw, err := execCommand("dmidecode", "-t", "system")
	if err != nil {
		return errMsg{err}
	}

	re := regexp.MustCompile(`Serial Number:\s*(.+)`)
	matches := re.FindStringSubmatch(dmidecodeRaw)
	if len(matches) > 1 {
		sysInfo.SerialNumber = strings.TrimSpace(matches[1])
	}

	return sysInfoCollectedMsg{
		sysInfo:      sysInfo,
		dmidecodeRaw: dmidecodeRaw,
	}
}

type sysInfoCollectedMsg struct {
	sysInfo      SystemInfo
	dmidecodeRaw string
}

// Функции сбора данных о системе
func getProcessorInfo() (ProcessorInfo, error) {
	var info ProcessorInfo

	// Получаем информацию из /proc/cpuinfo
	cpuinfo, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return info, err
	}

	// Получаем модель процессора
	modelRegex := regexp.MustCompile(`model name\s*:\s*(.+)`)
	model := modelRegex.FindSubmatch(cpuinfo)
	if len(model) > 1 {
		info.Model = strings.TrimSpace(string(model[1]))
	}

	// Получаем количество физических ядер
	physicalCoresCmd := exec.Command("sh", "-c", "grep 'cpu cores' /proc/cpuinfo | uniq | awk '{print $4}'")
	physicalCoresOutput, err := physicalCoresCmd.Output()
	if err == nil && len(strings.TrimSpace(string(physicalCoresOutput))) > 0 {
		info.Cores, _ = strconv.Atoi(strings.TrimSpace(string(physicalCoresOutput)))
	}

	// Если не удалось получить количество ядер, считаем уникальные physical id
	if info.Cores == 0 {
		physicalCoresCmd = exec.Command("sh", "-c", "cat /proc/cpuinfo | grep 'physical id' | sort -u | wc -l")
		physicalCoresOutput, err := physicalCoresCmd.Output()
		if err == nil && len(strings.TrimSpace(string(physicalCoresOutput))) > 0 {
			info.Cores, _ = strconv.Atoi(strings.TrimSpace(string(physicalCoresOutput)))
		}
	}

	// Получаем количество логических ядер
	threadsCmd := exec.Command("sh", "-c", "cat /proc/cpuinfo | grep processor | wc -l")
	threadsOutput, err := threadsCmd.Output()
	if err == nil {
		info.Threads, _ = strconv.Atoi(strings.TrimSpace(string(threadsOutput)))
	}

	// Исправленный метод определения частоты CPU
	// Сначала пробуем scaling_max_freq
	freqCmd := exec.Command("sh", "-c", "cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_max_freq 2>/dev/null || echo ''")
	freqOutput, err := freqCmd.Output()
	if err == nil && len(strings.TrimSpace(string(freqOutput))) > 0 {
		freqKHz, _ := strconv.ParseFloat(strings.TrimSpace(string(freqOutput)), 64)
		info.Frequency = fmt.Sprintf("%.1f GHz", freqKHz/1000000.0)
	} else {
		// Пробуем через lscpu
		lscpuCmd := exec.Command("sh", "-c", "lscpu | grep 'CPU MHz' | head -1 | awk '{print $3}'")
		lscpuOutput, err := lscpuCmd.Output()
		if err == nil && len(strings.TrimSpace(string(lscpuOutput))) > 0 {
			freqMHz, _ := strconv.ParseFloat(strings.TrimSpace(string(lscpuOutput)), 64)
			info.Frequency = fmt.Sprintf("%.1f GHz", freqMHz/1000.0)
		} else {
			// Пробуем напрямую из /proc/cpuinfo
			cpuFreqRegex := regexp.MustCompile(`cpu MHz\s*:\s*([0-9.]+)`)
			cpuFreqMatch := cpuFreqRegex.FindSubmatch(cpuinfo)
			if len(cpuFreqMatch) > 1 {
				freqMHz, _ := strconv.ParseFloat(strings.TrimSpace(string(cpuFreqMatch[1])), 64)
				info.Frequency = fmt.Sprintf("%.1f GHz", freqMHz/1000.0)
			} else {
				info.Frequency = "Unknown"
			}
		}
	}

	// Получаем информацию о кэше
	info.Cache = make(map[string]string)

	// L1 кэш
	l1dCacheCmd := exec.Command("sh", "-c", "lscpu | grep 'L1d cache' | awk '{print $3, $4}'")
	l1dCacheOutput, _ := l1dCacheCmd.Output()
	l1iCacheCmd := exec.Command("sh", "-c", "lscpu | grep 'L1i cache' | awk '{print $3, $4}'")
	l1iCacheOutput, _ := l1iCacheCmd.Output()

	if len(l1dCacheOutput) > 0 && len(l1iCacheOutput) > 0 {
		info.Cache["L1"] = strings.TrimSpace(string(l1dCacheOutput))
	}

	// L2 кэш
	l2CacheCmd := exec.Command("sh", "-c", "lscpu | grep 'L2 cache' | awk '{print $3, $4}'")
	l2CacheOutput, _ := l2CacheCmd.Output()
	if len(l2CacheOutput) > 0 {
		info.Cache["L2"] = strings.TrimSpace(string(l2CacheOutput))
	}

	// L3 кэш
	l3CacheCmd := exec.Command("sh", "-c", "lscpu | grep 'L3 cache' | awk '{print $3, $4}'")
	l3CacheOutput, _ := l3CacheCmd.Output()
	if len(l3CacheOutput) > 0 {
		info.Cache["L3"] = strings.TrimSpace(string(l3CacheOutput))
	}

	return info, nil
}

func getMemoryInfo() (MemoryInfo, error) {
	var info MemoryInfo

	// Получаем общий объем памяти
	meminfo, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return info, err
	}

	totalRegex := regexp.MustCompile(`MemTotal:\s*(\d+)`)
	total := totalRegex.FindSubmatch(meminfo)
	if len(total) > 1 {
		totalKB, _ := strconv.ParseInt(string(total[1]), 10, 64)
		info.Total = fmt.Sprintf("%d GB", totalKB/1024/1024)
	}

	// Получаем информацию о слотах памяти из dmidecode
	output, err := execCommand("dmidecode", "-t", "memory")
	if err != nil {
		return info, err
	}

	// Разделяем вывод на блоки Memory Device
	blocks := strings.Split(output, "Memory Device")

	for i, block := range blocks {
		if i == 0 { // Пропускаем заголовок
			continue
		}

		// Проверяем есть ли модуль в слоте
		if strings.Contains(block, "No Module Installed") {
			continue
		}

		// Размер
		sizeRegex := regexp.MustCompile(`Size: ([^\n]+)`)
		size := sizeRegex.FindStringSubmatch(block)
		if len(size) > 1 && !strings.Contains(size[1], "No Module Installed") {
			slot := MemorySlot{
				ID:   fmt.Sprintf("%d", i),
				Size: strings.TrimSpace(size[1]),
			}

			// Тип памяти
			typeRegex := regexp.MustCompile(`Type: ([^\n]+)`)
			typeMatch := typeRegex.FindStringSubmatch(block)
			if len(typeMatch) > 1 {
				slot.Type = strings.TrimSpace(typeMatch[1])
			}

			// Скорость
			speedRegex := regexp.MustCompile(`Speed: ([^\n]+)`)
			speedMatch := speedRegex.FindStringSubmatch(block)
			if len(speedMatch) > 1 {
				slot.Speed = strings.TrimSpace(speedMatch[1])
			}

			// Производитель
			mfgRegex := regexp.MustCompile(`Manufacturer: ([^\n]+)`)
			mfgMatch := mfgRegex.FindStringSubmatch(block)
			if len(mfgMatch) > 1 {
				slot.Manufacturer = strings.TrimSpace(mfgMatch[1])
			}

			info.Slots = append(info.Slots, slot)
		}
	}

	return info, nil
}

func getNetworkInfo() ([]NetworkInfo, error) {
	var interfaces []NetworkInfo

	// Получаем список сетевых интерфейсов
	netDir := "/sys/class/net/"
	files, err := os.ReadDir(netDir)
	if err != nil {
		return interfaces, err
	}

	for _, file := range files {
		ifName := file.Name()
		if ifName == "lo" {
			continue // Пропускаем локальный интерфейс
		}

		netInfo := NetworkInfo{
			Interface: ifName,
		}

		// Получаем MAC адрес
		macBytes, err := os.ReadFile(filepath.Join(netDir, ifName, "address"))
		if err == nil {
			netInfo.MAC = strings.TrimSpace(string(macBytes))
		}

		// Получаем модель устройства через lspci
		devicePath, err := os.Readlink(filepath.Join(netDir, ifName, "device"))
		if err == nil {
			// Получаем информацию о производителе устройства через lspci
			busID := filepath.Base(devicePath)
			vendorInfoCmd := exec.Command("sh", "-c", fmt.Sprintf("lspci -v -s %s | grep -i 'Subsystem'", busID))
			vendorOutput, err := vendorInfoCmd.Output()
			if err == nil && len(vendorOutput) > 0 {
				netInfo.Model = strings.TrimSpace(strings.Replace(string(vendorOutput), "Subsystem:", "", 1))
			} else {
				// Пробуем получить информацию с помощью lshw
				lshwCmd := exec.Command("sh", "-c", fmt.Sprintf("lshw -c network -businfo | grep %s | head -1", ifName))
				lshwOutput, err := lshwCmd.Output()
				if err == nil && len(lshwOutput) > 0 {
					parts := strings.Fields(string(lshwOutput))
					if len(parts) >= 3 {
						netInfo.Model = parts[2]
					}
				}
			}
		}

		// Если все еще нет модели, попробуем через ethtool
		if netInfo.Model == "" {
			ethtoolCmd := exec.Command("ethtool", "-i", ifName)
			ethtoolOutput, err := ethtoolCmd.Output()
			if err == nil {
				lines := strings.Split(string(ethtoolOutput), "\n")
				var driverInfo, versionInfo string

				for _, line := range lines {
					if strings.HasPrefix(line, "driver:") {
						parts := strings.SplitN(line, ":", 2)
						if len(parts) > 1 {
							driverInfo = strings.TrimSpace(parts[1])
						}
					} else if strings.HasPrefix(line, "version:") {
						parts := strings.SplitN(line, ":", 2)
						if len(parts) > 1 {
							versionInfo = strings.TrimSpace(parts[1])
						}
					} else if strings.HasPrefix(line, "firmware-version:") {
						parts := strings.SplitN(line, ":", 2)
						if len(parts) > 1 {
							// Добавляем версию прошивки, если доступна
							versionInfo += " (fw: " + strings.TrimSpace(parts[1]) + ")"
						}
					}
				}

				if driverInfo != "" {
					netInfo.Model = driverInfo
					if versionInfo != "" {
						netInfo.Model += " " + versionInfo
					}
				}
			}
		}

		// Если до сих пор не получили модель, используем общее название
		if netInfo.Model == "" {
			netInfo.Model = "Network Interface"
		}

		interfaces = append(interfaces, netInfo)
	}

	return interfaces, nil
}

func getGPUInfo() (GPUInfo, error) {
	var info GPUInfo

	// Пробуем использовать lspci для получения информации о GPU
	cmd := exec.Command("sh", "-c", "lspci | grep -i 'vga\\|3d\\|2d'")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		info.Model = strings.TrimSpace(string(output))

		// Получаем дополнительную информацию о GPU

		// 1. Пробуем glxinfo для получения общей информации
		glxInfoCmd := exec.Command("sh", "-c", "glxinfo | grep -E 'OpenGL vendor|OpenGL renderer|OpenGL version'")
		glxInfoOutput, err := glxInfoCmd.Output()
		if err == nil && len(glxInfoOutput) > 0 {
			glxLines := strings.Split(string(glxInfoOutput), "\n")
			for _, line := range glxLines {
				if strings.Contains(line, "OpenGL vendor") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) > 1 {
						info.Vendor = strings.TrimSpace(parts[1])
					}
				} else if strings.Contains(line, "OpenGL renderer") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) > 1 {
						if info.Model == "" {
							info.Model = strings.TrimSpace(parts[1])
						}
					}
				} else if strings.Contains(line, "OpenGL version") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) > 1 {
						info.OpenGLVersion = strings.TrimSpace(parts[1])
					}
				}
			}
		}

		// 2. Получаем разрешение экрана
		resolutionCmd := exec.Command("sh", "-c", "xrandr --current | grep '*' | awk '{print $1}'")
		resolutionOutput, err := resolutionCmd.Output()
		if err == nil && len(resolutionOutput) > 0 {
			info.Resolution = strings.TrimSpace(string(resolutionOutput))
		}

		// 3. Пробуем nvidia-smi для NVIDIA карт
		nvidiaCmd := exec.Command("sh", "-c", "nvidia-smi --query-gpu=name,memory.total,architecture --format=csv,noheader")
		nvidiaOutput, err := nvidiaCmd.Output()
		if err == nil && len(nvidiaOutput) > 0 {
			parts := strings.Split(string(nvidiaOutput), ",")
			if len(parts) >= 2 {
				info.Model = strings.TrimSpace(parts[0])
				info.Memory = strings.TrimSpace(parts[1])

				if len(parts) >= 3 {
					info.Architecture = strings.TrimSpace(parts[2])
				}

				// Получаем версию драйвера
				driverCmd := exec.Command("sh", "-c", "nvidia-smi --query-gpu=driver_version --format=csv,noheader")
				driverOutput, err := driverCmd.Output()
				if err == nil && len(driverOutput) > 0 {
					info.Driver = fmt.Sprintf("NVIDIA %s", strings.TrimSpace(string(driverOutput)))
				}
			}
		} else {
			// Пробуем для AMD карт
			amdCmd := exec.Command("sh", "-c", "lspci -v | grep -A 10 VGA | grep -i amdgpu")
			amdOutput, err := amdCmd.Output()
			if err == nil && len(amdOutput) > 0 {
				// Если это AMD карта, пытаемся получить версию драйвера
				amdDriverCmd := exec.Command("sh", "-c", "grep -i 'amdgpu' /var/log/Xorg.0.log | grep 'Driver for'")
				amdDriverOutput, err := amdDriverCmd.Output()
				if err == nil && len(amdDriverOutput) > 0 {
					info.Driver = strings.TrimSpace(string(amdDriverOutput))
				} else {
					info.Driver = "AMD GPU Driver"
				}

				// Дополнительно пробуем получить архитектуру AMD GPU
				amdArchCmd := exec.Command("sh", "-c", "lspci -v | grep -A 20 VGA | grep -i 'Architecture'")
				amdArchOutput, _ := amdArchCmd.Output()
				if len(amdArchOutput) > 0 {
					info.Architecture = strings.TrimSpace(string(amdArchOutput))
				}
			} else {
				// Проверяем Intel Graphics
				intelCmd := exec.Command("sh", "-c", "lspci -v | grep -A 10 VGA | grep -i intel")
				intelOutput, err := intelCmd.Output()
				if err == nil && len(intelOutput) > 0 {
					info.Driver = "Intel Graphics Driver"

					// Пытаемся получить версию драйвера Intel
					intelVersionCmd := exec.Command("sh", "-c", "grep -i 'intel' /var/log/Xorg.0.log | grep 'version'")
					intelVersionOutput, _ := intelVersionCmd.Output()
					if len(intelVersionOutput) > 0 {
						info.Driver = strings.TrimSpace(string(intelVersionOutput))
					}
				}
			}
		}
	}

	return info, nil
}

func getStorageInfo() ([]StorageInfo, error) {
	var storageDevices []StorageInfo

	// Используем lsblk для получения информации о дисках
	cmd := exec.Command("sh", "-c", "lsblk -o NAME,SIZE,TYPE,MODEL,MOUNTPOINT,LABEL -J")
	output, err := cmd.Output()
	if err != nil {
		// Попробуем альтернативный вариант без -J (JSON форматирования)
		cmd = exec.Command("sh", "-c", "lsblk -o NAME,SIZE,TYPE,MODEL,MOUNTPOINT,LABEL")
		output, err = cmd.Output()
		if err != nil {
			return storageDevices, err
		}

		// Парсим текстовый вывод lsblk
		lines := strings.Split(string(output), "\n")
		if len(lines) > 1 { // Пропускаем заголовок
			for i := 1; i < len(lines); i++ {
				fields := strings.Fields(lines[i])
				if len(fields) >= 3 && fields[2] == "disk" {
					device := StorageInfo{
						Type: "SATA/IDE",
						Size: fields[1],
					}

					if len(fields) >= 4 {
						device.Model = fields[3]
					}

					if strings.HasPrefix(fields[0], "nvme") {
						device.Type = "NVMe"
					} else if strings.HasPrefix(fields[0], "sd") {
						// Проверяем, USB это или SATA
						symlinkPath := fmt.Sprintf("/sys/block/%s", fields[0])
						realPath, err := filepath.EvalSymlinks(symlinkPath)
						if err == nil {
							if strings.Contains(realPath, "usb") {
								device.Type = "USB"
							}
						}
					} else if strings.HasPrefix(fields[0], "mmcblk") {
						device.Type = "SD/MMC"
					}

					// Ищем метку в выводе lsblk
					if len(fields) >= 6 {
						device.Label = fields[5]
					}

					storageDevices = append(storageDevices, device)
				}
			}
		}

		return storageDevices, nil
	}

	// Парсим JSON от lsblk
	var lsblkOutput struct {
		Blockdevices []struct {
			Name       string `json:"name"`
			Size       string `json:"size"`
			Type       string `json:"type"`
			Model      string `json:"model"`
			Mountpoint string `json:"mountpoint"`
			Label      string `json:"label"`
			Children   []struct {
				Name       string `json:"name"`
				Size       string `json:"size"`
				Type       string `json:"type"`
				Mountpoint string `json:"mountpoint"`
				Label      string `json:"label"`
			} `json:"children,omitempty"`
		} `json:"blockdevices"`
	}

	err = json.Unmarshal(output, &lsblkOutput)
	if err != nil {
		return storageDevices, err
	}

	// Обрабатываем полученные данные
	for _, device := range lsblkOutput.Blockdevices {
		if device.Type == "disk" || device.Type == "rom" {
			storageType := "SATA/IDE"

			// Определяем тип устройства (NVMe, USB, и т.д.)
			if strings.HasPrefix(device.Name, "nvme") {
				storageType = "NVMe"
			} else if strings.HasPrefix(device.Name, "sd") {
				// Проверяем, USB это или SATA
				symlinkPath := fmt.Sprintf("/sys/block/%s", device.Name)
				realPath, err := filepath.EvalSymlinks(symlinkPath)
				if err == nil {
					if strings.Contains(realPath, "usb") {
						storageType = "USB"
					}
				}
			} else if strings.HasPrefix(device.Name, "mmcblk") {
				storageType = "SD/MMC"
			}

			storage := StorageInfo{
				Type:  storageType,
				Model: device.Model,
				Size:  device.Size,
			}

			// Ищем метку в разделах, если она есть
			for _, partition := range device.Children {
				if partition.Label != "" {
					storage.Label = partition.Label
					break
				}
			}

			storageDevices = append(storageDevices, storage)
		}
	}

	return storageDevices, nil
}

// Вспомогательная функция для выполнения команд
func execCommand(command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput() // Объединяем stdout и stderr
	if err != nil {
		return "", fmt.Errorf("ошибка выполнения команды %s: %v\nВывод: %s", command, err, string(output))
	}
	return string(output), nil
}

// Команда для запуска видео теста в терминале (без ffplay)
func startVideoTestCmd() tea.Msg {
	return startVideoTestMsg{}
}

type startVideoTestMsg struct{}

// Таймер для видеотеста
func videoTestTimerCmd() tea.Msg {
	return videoTestTimerTickMsg{}
}

type videoTestTimerTickMsg struct{}

// Окончание видеотеста
type videoTestDoneMsg struct{}

// Перезапуск системной информации
type restartSystemInfoMsg struct{}

// Проверка серийного номера
func checkSerialNumberCmd(entered, system string) tea.Msg {
	if entered == system {
		return serialMatchedMsg{}
	}
	return serialMismatchMsg{
		entered: entered,
		system:  system,
	}
}

type serialMatchedMsg struct{}
type serialMismatchMsg struct {
	entered string
	system  string
}

// Перезапустить компьютер
type restartMsg struct{}

// Выключить компьютер
type shutdownMsg struct{}

// Команда для создания логов
func createLogFilesCmd(info SystemInfo, dmidecodeRaw string, testPassed bool, serialMatched bool) tea.Msg {
	// Создаем директорию для логов
	logsDir := "./troubadour_logs"
	err := os.MkdirAll(logsDir, 0755)
	if err != nil {
		return errMsg{err}
	}

	// Создаем имя файла с датой и серийным номером
	timestamp := time.Now().Format("20060102_150405")
	fileName := fmt.Sprintf("%s/troubadour_%s_%s.log", logsDir, info.SerialNumber, timestamp)

	// Форматируем содержимое лога
	var logContent strings.Builder

	logContent.WriteString("==== TROUBADOUR SYSTEM DIAGNOSTICS LOG ====\n\n")
	logContent.WriteString(fmt.Sprintf("Date: %s\n", time.Now().Format(time.RFC1123)))
	logContent.WriteString(fmt.Sprintf("Serial Number: %s\n\n", info.SerialNumber))

	// Информация о процессоре
	logContent.WriteString("==== PROCESSOR ====\n")
	logContent.WriteString(fmt.Sprintf("Model: %s\n", info.Processor.Model))
	logContent.WriteString(fmt.Sprintf("Cores: %d (Threads: %d)\n", info.Processor.Cores, info.Processor.Threads))
	logContent.WriteString(fmt.Sprintf("Frequency: %s\n", info.Processor.Frequency))

	logContent.WriteString("Cache:\n")
	for level, size := range info.Processor.Cache {
		logContent.WriteString(fmt.Sprintf("  %s: %s\n", level, size))
	}
	logContent.WriteString("\n")

	// Информация о памяти
	logContent.WriteString("==== MEMORY ====\n")
	logContent.WriteString(fmt.Sprintf("Total: %s\n", info.Memory.Total))

	for _, slot := range info.Memory.Slots {
		logContent.WriteString(fmt.Sprintf("Slot %s: %s %s @ %s [%s]\n",
			slot.ID, slot.Manufacturer, slot.Size, slot.Speed, slot.Type))
	}
	logContent.WriteString("\n")

	// Информация о сетевых картах
	logContent.WriteString("==== NETWORK ====\n")
	for _, net := range info.Network {
		logContent.WriteString(fmt.Sprintf("Interface: %s\n", net.Interface))
		logContent.WriteString(fmt.Sprintf("Model: %s\n", net.Model))
		logContent.WriteString(fmt.Sprintf("MAC: %s\n\n", net.MAC))
	}

	// Информация о GPU
	logContent.WriteString("==== GPU ====\n")
	logContent.WriteString(fmt.Sprintf("Model: %s\n", info.GPU.Model))
	if info.GPU.Memory != "" {
		logContent.WriteString(fmt.Sprintf("Memory: %s\n", info.GPU.Memory))
	}
	if info.GPU.Driver != "" {
		logContent.WriteString(fmt.Sprintf("Driver: %s\n", info.GPU.Driver))
	}
	if info.GPU.Vendor != "" {
		logContent.WriteString(fmt.Sprintf("Vendor: %s\n", info.GPU.Vendor))
	}
	if info.GPU.Architecture != "" {
		logContent.WriteString(fmt.Sprintf("Architecture: %s\n", info.GPU.Architecture))
	}
	if info.GPU.Resolution != "" {
		logContent.WriteString(fmt.Sprintf("Resolution: %s\n", info.GPU.Resolution))
	}
	if info.GPU.OpenGLVersion != "" {
		logContent.WriteString(fmt.Sprintf("OpenGL Version: %s\n", info.GPU.OpenGLVersion))
	}
	logContent.WriteString("\n")

	// Информация о накопителях
	logContent.WriteString("==== STORAGE ====\n")
	for _, storage := range info.Storage {
		logContent.WriteString(fmt.Sprintf("Type: %s\n", storage.Type))
		logContent.WriteString(fmt.Sprintf("Model: %s\n", storage.Model))
		logContent.WriteString(fmt.Sprintf("Size: %s\n", storage.Size))
		if storage.Label != "" {
			logContent.WriteString(fmt.Sprintf("Label: %s\n", storage.Label))
		}
		logContent.WriteString("\n")
	}

	// Информация о пройденных этапах
	logContent.WriteString("==== TEST RESULTS ====\n")
	logContent.WriteString(fmt.Sprintf("Video Test Passed: %t\n", testPassed))
	logContent.WriteString(fmt.Sprintf("Serial Number Check: %t\n", serialMatched))
	logContent.WriteString(fmt.Sprintf("Entered Serial Number: %s\n", info.SerialNumber))
	logContent.WriteString(fmt.Sprintf("System Serial Number: %s\n\n", info.SerialNumber))

	// Добавляем сырой вывод dmidecode
	logContent.WriteString("==== RAW DMIDECODE DATA ====\n")
	logContent.WriteString(dmidecodeRaw)

	// Записываем лог в файл
	err = os.WriteFile(fileName, []byte(logContent.String()), 0644)
	if err != nil {
		return errMsg{err}
	}

	return logCreatedMsg{
		fileName: fileName,
	}
}

type logCreatedMsg struct {
	fileName string
}

// Дополнительные флаги для видеотеста
type testWaitingForInput struct{}
type testPatternFinishMsg struct{}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Если тест ожидает ввода пользователя, любая клавиша завершает тест
		if m.videoTestActive && m.videoTestColor == 3 {
			m.videoTestActive = false
			m.state = stateAskVideoOk
			m.showOverlay = true
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "enter":
			switch m.state {
			case stateShowInfo:
				// Переходим к видео тесту
				m.state = stateVideoTest
				m.videoTestActive = true
				m.videoTestColor = 0
				m.videoTestStart = time.Now()
				return m, startVideoTestCmd

			case stateAskVideoOk:
				// Если ответ "Y" (по умолчанию), продолжаем к проверке серийника
				m.state = stateAskSerial
				m.showOverlay = true
				m.testPassed = true // Пользователь подтвердил успешное прохождение теста
				return m, nil

			case stateAskSerial:
				// Проверяем серийный номер
				m.userSerial = m.textInput.Value()
				m.state = stateCheckSerial
				return m, func() tea.Msg {
					return checkSerialNumberCmd(m.userSerial, m.sysInfo.SerialNumber)
				}

			case stateSerialSuccess:
				// Переходим к созданию логов после успешной проверки серийного номера
				m.state = stateCreateLogs
				m.showOverlay = true
				return m, func() tea.Msg {
					return createLogFilesCmd(m.sysInfo, m.dmidecodeRaw, m.testPassed, true)
				}

			case stateSerialError:
				// Повторная проверка серийника
				m.state = stateAskSerial
				m.textInput.SetValue("")
				return m, nil

			case stateDone:
				// Завершаем программу
				return m, tea.Quit
			}

		case "n":
			if m.state == stateAskVideoOk {
				// Повторяем тест
				m.state = stateVideoTest
				m.videoTestActive = true
				m.videoTestColor = 0
				m.videoTestStart = time.Now()
				m.testPassed = false // Тест не пройден
				return m, startVideoTestCmd
			}

		case "r":
			if m.state == stateSerialError {
				// Перезапуск системы
				return m, func() tea.Msg {
					exec.Command("reboot").Run()
					return restartMsg{}
				}
			}

		case "e":
			if m.state == stateSerialError {
				// Выключение системы
				return m, func() tea.Msg {
					exec.Command("poweroff").Run()
					return shutdownMsg{}
				}
			}

		case "b":
			// Возврат к экрану системной информации из определенных состояний
			if m.state != stateInit && m.state != stateShowInfo && m.state != stateAskSerial {
				m.state = stateShowInfo
				m.showOverlay = false
				m.videoTestActive = false
				return m, nil
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 6 // Учитываем место для заголовка и подвала

	case errMsg:
		m.err = msg.error
		return m, tea.Quit

	case sysInfoCollectedMsg:
		m.sysInfo = msg.sysInfo
		m.dmidecodeRaw = msg.dmidecodeRaw
		m.state = stateShowInfo
		return m, nil

	case startVideoTestMsg:
		// Запускаем таймер для смены цветов в видеотесте
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg {
			return videoTestTimerTickMsg{}
		})

	case videoTestTimerTickMsg:
		if m.videoTestActive {
			elapsedSeconds := int(time.Since(m.videoTestStart).Seconds())

			// Последняя фаза теста - SMPTE таблица - ожидает ввода пользователя
			if m.videoTestColor == 3 {
				// Продолжаем показывать тестовую таблицу, ожидая ввода
				return m, tea.Tick(time.Second, func(time.Time) tea.Msg {
					return videoTestTimerTickMsg{}
				})
			}

			// Каждую секунду меняем цвет (красный -> зеленый -> синий -> тестовая таблица)
			// Всего 3 секунды на цвета, потом тестовая таблица
			if elapsedSeconds >= 3 {
				// Переходим к тестовой таблице SMPTE и ждем ввода пользователя
				m.videoTestColor = 3 // Устанавливаем последний тестовый паттерн
				// Продолжаем таймер для обновления оставшегося времени
				return m, tea.Tick(time.Second, func(time.Time) tea.Msg {
					return videoTestTimerTickMsg{}
				})
			} else {
				// Меняем цвет каждую секунду
				m.videoTestColor = elapsedSeconds % 3 // Только первые три цвета
				// Продолжаем таймер
				return m, tea.Tick(time.Second, func(time.Time) tea.Msg {
					return videoTestTimerTickMsg{}
				})
			}
		}
		return m, nil

	case videoTestDoneMsg:
		m.videoTestActive = false
		m.state = stateAskVideoOk
		return m, nil

	case serialMatchedMsg:
		// Серийный номер совпал, показываем сообщение об успехе
		m.state = stateSerialSuccess
		m.showOverlay = true
		m.serialMatched = true
		return m, nil

	case serialMismatchMsg:
		// Серийный номер не совпал, показываем ошибку
		m.state = stateSerialError
		m.userSerial = msg.entered
		m.showOverlay = true
		m.serialMatched = false
		return m, nil

	case logCreatedMsg:
		m.state = stateDone
		m.logFilePath = msg.fileName
		return m, nil
	}

	// Обновляем компоненты
	switch m.state {
	case stateInit:
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case stateAskSerial:
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) View() string {
	// Стили для отображения
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Background(lipgloss.Color("#1D1D1D")).
		Padding(1, 0, 0, 0).
		Width(m.width).
		Align(lipgloss.Center)

	// Уменьшаем внутренние отступы для основных контейнеров
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#3C3C3C")).
		Padding(0, 0).
		Width(m.width - 2)

	// Изменяем стили секций для более точного контроля размеров
	sectionStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3C3C3C")).
		Padding(0, 1)

	sectionTitleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#E8E8E8"))

	errorStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#FF0000")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#880000")).
		Padding(1, 2).
		Align(lipgloss.Center)

	successStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#00AA00")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#008800")).
		Padding(1, 2).
		Align(lipgloss.Center)

	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#CDCDCD")).
		Padding(0, 1).
		Width(m.width)

	overlayStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#222222")).
		Padding(2, 4).
		Align(lipgloss.Center)

	// Если активен видеотест, показываем его на весь экран
	if m.videoTestActive {
		var colorBg string
		var colorName string
		var progressInfo string

		switch m.videoTestColor {
		case 0:
			colorBg = "#FF0000"
			colorName = "RED"
			progressInfo = fmt.Sprintf(
				"Testing video... %s (1/4) [%d sec remaining]",
				colorName,
				3-int(time.Since(m.videoTestStart).Seconds()),
			)
		case 1:
			colorBg = "#00FF00"
			colorName = "GREEN"
			progressInfo = fmt.Sprintf(
				"Testing video... %s (2/4) [%d sec remaining]",
				colorName,
				3-int(time.Since(m.videoTestStart).Seconds()),
			)
		case 2:
			colorBg = "#0000FF"
			colorName = "BLUE"
			progressInfo = fmt.Sprintf(
				"Testing video... %s (3/4) [%d sec remaining]",
				colorName,
				3-int(time.Since(m.videoTestStart).Seconds()),
			)
		case 3:
			// Настроечная таблица SMPTE HD на весь экран
			return drawSMPTETestPattern(m.width, m.height, 0)
		}

		// Создаем фон на весь экран с соответствующим цветом
		testBg := lipgloss.NewStyle().
			Background(lipgloss.Color(colorBg)).
			Width(m.width).
			Height(m.height - 1) // Используем весь экран, оставляя только одну строку для информации

		return fmt.Sprintf(
			"%s\n%s",
			testBg.Render(""),
			lipgloss.NewStyle().
				Align(lipgloss.Center).
				Width(m.width).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#000000")).
				Render(progressInfo),
		)
	}

	// Улучшенный расчет размеров для отображения
	headerHeight := 1
	footerHeight := 1
	contentHeight := m.height - headerHeight - footerHeight - 2

	if m.state == stateInit {
		spinnerContent := fmt.Sprintf(
			"%s\n\n%s",
			lipgloss.NewStyle().Align(lipgloss.Center).Width(m.width-2).Render("Collecting system information..."),
			lipgloss.NewStyle().Align(lipgloss.Center).Width(m.width-2).Render(m.spinner.View()),
		)

		return lipgloss.JoinVertical(
			lipgloss.Left,
			titleStyle.Render("TROUBADOUR"),
			borderStyle.Copy().Height(contentHeight).Render(spinnerContent),
		)
	}

	// Если произошла ошибка
	if m.err != nil {
		errorContent := fmt.Sprintf(
			"%s\n\n%s",
			errorStyle.Render(fmt.Sprintf("ERROR: %v", m.err)),
			footerStyle.Render("Press any key to exit"),
		)

		return lipgloss.JoinVertical(
			lipgloss.Left,
			titleStyle.Render("TROUBADOUR"),
			borderStyle.Copy().Height(contentHeight).Render(errorContent),
		)
	}

	// ИСПРАВЛЕННАЯ ВЕРСИЯ ОТОБРАЖЕНИЯ СИСТЕМНОЙ ИНФОРМАЦИИ

	// Определяем, будем ли использовать две колонки
	// Используем две колонки, только если экран достаточно широкий
	twoColumnMode := m.width >= 100

	// Расчет размеров колонок с учетом границ и отступов
	columnPadding := 1 // Отступ внутри колонки
	columnSpacing := 2 // Расстояние между колонками
	borderWidth := 2   // Ширина границы

	// Доступная ширина для контента с учетом основной границы
	availableWidth := m.width - (borderWidth * 2)

	var leftColumnWidth, rightColumnWidth int

	if twoColumnMode {
		// В режиме двух колонок делим пространство пополам
		leftColumnWidth = (availableWidth - columnSpacing) / 2
		rightColumnWidth = (availableWidth - columnSpacing) / 2
	} else {
		// В режиме одной колонки используем всю доступную ширину
		leftColumnWidth = availableWidth
		rightColumnWidth = availableWidth
	}

	// Ширина для секций внутри колонок (с учетом отступов и границ секций)
	sectionBorderWidth := 2
	leftSectionWidth := leftColumnWidth - (sectionBorderWidth * 2) - (columnPadding * 2)
	rightSectionWidth := rightColumnWidth - (sectionBorderWidth * 2) - (columnPadding * 2)

	// Создаем лого Troubadour
	logoContent := strings.Builder{}
	logoContent.WriteString("     ♪ ♫ ♪\n")
	logoContent.WriteString("    /T\\    \n")
	logoContent.WriteString("   /___\\   \n")
	logoContent.WriteString("  // | \\\\  \n")
	logoContent.WriteString(" //__|__\\\\ \n")
	logoContent.WriteString("    |||    \n")
	logoContent.WriteString("  ~=====~  \n")

	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F5D76E")).
		Align(lipgloss.Center).
		Width(leftColumnWidth - 2)

	logoSection := logoStyle.Render(logoContent.String())

	// Формируем левую колонку
	// ПРОЦЕССОР
	cpuContent := strings.Builder{}
	cpuContent.WriteString(fmt.Sprintf("Model: %s\n", truncateString(m.sysInfo.Processor.Model, leftSectionWidth-8)))
	cpuContent.WriteString(fmt.Sprintf("Cores: %d (Threads: %d)\n", m.sysInfo.Processor.Cores, m.sysInfo.Processor.Threads))
	cpuContent.WriteString(fmt.Sprintf("Freq: %s\n", m.sysInfo.Processor.Frequency))

	cacheStr := ""
	for level, size := range m.sysInfo.Processor.Cache {
		if cacheStr != "" {
			cacheStr += ", "
		}
		cacheStr += fmt.Sprintf("%s: %s", level, size)
	}
	cpuContent.WriteString(fmt.Sprintf("Cache: %s", truncateString(cacheStr, leftSectionWidth-8)))

	procSection := sectionStyle.Copy().
		Width(leftColumnWidth - 2).
		Render(fmt.Sprintf("%s\n%s",
			sectionTitleStyle.Render("─── PROCESSOR ───"),
			cpuContent.String(),
		))

	// СЕТЬ
	netContent := strings.Builder{}
	for _, net := range m.sysInfo.Network {
		if net.Model != "" {
			netContent.WriteString(fmt.Sprintf("%s: %s\n",
				net.Interface,
				truncateString(net.Model, leftSectionWidth-len(net.Interface)-3)))
		} else {
			netContent.WriteString(fmt.Sprintf("%s\n", net.Interface))
		}
		netContent.WriteString(fmt.Sprintf("MAC: %s\n\n", net.MAC))
	}

	netSection := sectionStyle.Copy().
		Width(leftColumnWidth - 2).
		Render(fmt.Sprintf("%s\n%s",
			sectionTitleStyle.Render("─── NETWORK ───"),
			netContent.String(),
		))

	// Формируем правую колонку
	// ПАМЯТЬ
	memContent := strings.Builder{}
	memContent.WriteString(fmt.Sprintf("Total: %s\n\n", m.sysInfo.Memory.Total))

	for _, slot := range m.sysInfo.Memory.Slots {
		slotManu := strings.TrimSpace(strings.ReplaceAll(slot.Manufacturer, "\n", " "))
		slotSize := strings.TrimSpace(strings.ReplaceAll(slot.Size, "\n", " "))
		slotSpeed := strings.TrimSpace(strings.ReplaceAll(slot.Speed, "\n", " "))
		slotType := strings.TrimSpace(strings.ReplaceAll(slot.Type, "\n", " "))

		memContent.WriteString(fmt.Sprintf("Slot %s: %s %s\n",
			slot.ID,
			truncateString(slotManu, rightSectionWidth/4),
			slotSize))
		memContent.WriteString(fmt.Sprintf("        %s %s\n\n",
			slotSpeed,
			slotType))
	}

	memSection := sectionStyle.Copy().
		Width(rightColumnWidth - 2).
		Render(fmt.Sprintf("%s\n%s",
			sectionTitleStyle.Render("─── MEMORY ───"),
			memContent.String(),
		))

	// GPU
	gpuContent := strings.Builder{}
	gpuModel := strings.TrimSpace(strings.ReplaceAll(m.sysInfo.GPU.Model, "\n", " "))
	gpuContent.WriteString(fmt.Sprintf("Model: %s\n", truncateString(gpuModel, rightSectionWidth-8)))

	if m.sysInfo.GPU.Memory != "" {
		gpuMem := strings.TrimSpace(strings.ReplaceAll(m.sysInfo.GPU.Memory, "\n", " "))
		gpuContent.WriteString(fmt.Sprintf("Memory: %s\n", gpuMem))
	}

	if m.sysInfo.GPU.Driver != "" {
		gpuDriver := strings.TrimSpace(strings.ReplaceAll(m.sysInfo.GPU.Driver, "\n", " "))
		gpuContent.WriteString(fmt.Sprintf("Driver: %s\n", truncateString(gpuDriver, rightSectionWidth-9)))
	}

	if m.sysInfo.GPU.Vendor != "" {
		gpuVendor := strings.TrimSpace(strings.ReplaceAll(m.sysInfo.GPU.Vendor, "\n", " "))
		gpuContent.WriteString(fmt.Sprintf("Vendor: %s\n", truncateString(gpuVendor, rightSectionWidth-9)))
	}

	if m.sysInfo.GPU.Resolution != "" {
		gpuRes := strings.TrimSpace(strings.ReplaceAll(m.sysInfo.GPU.Resolution, "\n", " "))
		gpuContent.WriteString(fmt.Sprintf("Resolution: %s\n", gpuRes))
	}

	gpuSection := sectionStyle.Copy().
		Width(rightColumnWidth - 2).
		Render(fmt.Sprintf("%s\n%s",
			sectionTitleStyle.Render("─── GPU ───"),
			gpuContent.String(),
		))

	// ХРАНИЛИЩЕ
	storageContent := strings.Builder{}
	for _, storage := range m.sysInfo.Storage {
		storageContent.WriteString(fmt.Sprintf("%s: %s %s\n",
			storage.Type,
			truncateString(storage.Model, rightSectionWidth-len(storage.Type)-len(storage.Size)-3),
			storage.Size))

		if storage.Label != "" {
			storageContent.WriteString(fmt.Sprintf("Label: %s\n", storage.Label))
		}
		storageContent.WriteString("\n")
	}

	storageSection := sectionStyle.Copy().
		Width(rightColumnWidth - 2).
		Render(fmt.Sprintf("%s\n%s",
			sectionTitleStyle.Render("─── STORAGE ───"),
			storageContent.String(),
		))

	// Объединяем секции в колонки с точным позиционированием
	var leftColumn, rightColumn string

	leftColumn = lipgloss.JoinVertical(
		lipgloss.Left,
		logoSection,
		procSection,
		netSection,
	)

	rightColumn = lipgloss.JoinVertical(
		lipgloss.Left,
		memSection,
		gpuSection,
		storageSection,
	)

	// Формируем полное отображение
	var mainContent string

	if twoColumnMode {
		// В режиме двух колонок размещаем колонки рядом
		rows := make([]string, 0)

		leftRows := strings.Split(leftColumn, "\n")
		rightRows := strings.Split(rightColumn, "\n")

		maxRows := max(len(leftRows), len(rightRows))

		// Заполняем колонки пустыми строками до одинаковой длины
		for len(leftRows) < maxRows {
			leftRows = append(leftRows, "")
		}
		for len(rightRows) < maxRows {
			rightRows = append(rightRows, "")
		}

		// Объединяем строки из обеих колонок
		for i := 0; i < maxRows; i++ {
			leftPadded := leftRows[i]
			if len(leftPadded) < leftColumnWidth {
				leftPadded = leftPadded + strings.Repeat(" ", leftColumnWidth-len(leftPadded))
			}

			rows = append(rows, leftPadded+strings.Repeat(" ", columnSpacing)+rightRows[i])
		}

		mainContent = strings.Join(rows, "\n")
	} else {
		// В режиме одной колонки размещаем колонки одну под другой
		mainContent = leftColumn + "\n\n" + rightColumn
	}

	// Создаем финальное отображение
	footer := footerStyle.Render("Press ENTER to continue to video test...")
	if m.state != stateInit && m.state != stateShowInfo {
		footer = footerStyle.Render("Press ENTER to continue to video test... | Press B to return to system info")
	}

	baseView := lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render("TROUBADOUR"),
		borderStyle.Copy().Height(contentHeight).Render(mainContent),
		footer,
	)

	// Если не нужно показывать оверлей, возвращаем базовый вид
	if !m.showOverlay && m.state != stateVideoTest {
		return baseView
	}

	// Подготавливаем оверлей в зависимости от состояния
	var overlayContent string

	switch m.state {
	case stateAskVideoOk:
		overlayContent = fmt.Sprintf(
			"%s\n\n%s\n\n%s\n\n%s",
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00AAFF")).Render("Video Test Completed"),
			"Did all test patterns display correctly?",
			"[Y] Yes (default)   [n] No, run test again",
			"[B] Return to system information",
		)

	case stateAskSerial:
		overlayContent = fmt.Sprintf(
			"%s\n\n%s\n\n%s",
			lipgloss.NewStyle().Bold(true).Render("Serial Number Verification"),
			fmt.Sprintf("System Serial Number: %s", m.sysInfo.SerialNumber),
			fmt.Sprintf("Please enter Serial Number: %s", m.textInput.View()),
		)

	case stateSerialSuccess:
		// Новый случай для успешной проверки серийного номера
		successBox := successStyle.Width(45).Render(fmt.Sprintf(
			"Serial numbers match!\n\nSystem: %s\nEntered: %s\n\nPress ENTER to continue",
			m.sysInfo.SerialNumber, m.userSerial,
		))

		overlayContent = fmt.Sprintf(
			"%s\n\n%s\n\n%s",
			lipgloss.NewStyle().Bold(true).Render("Serial Number Verification Successful"),
			successBox,
			"[B] Return to system information",
		)

	case stateSerialError:
		errorBox := errorStyle.Width(45).Render(fmt.Sprintf(
			"Serial numbers DO NOT match!\n\nSystem: %s\nEntered: %s\n\n[R] Restart system\n[E] Shutdown system\n[ENTER] Try again",
			m.sysInfo.SerialNumber, m.userSerial,
		))

		overlayContent = fmt.Sprintf(
			"%s\n\n%s\n\n%s",
			lipgloss.NewStyle().Bold(true).Render("Serial Number Verification Failed"),
			errorBox,
			"[B] Return to system information",
		)

	case stateCreateLogs:
		overlayContent = fmt.Sprintf(
			"%s\n\n%s\n\n%s\n%s\n%s\n%s\n\n%s",
			lipgloss.NewStyle().Bold(true).Render("Log Creation"),
			"Creating system logs...",
			"■ Hardware information collected",
			"■ System verification completed",
			"■ dmidecode data parsed",
			"□ Writing log file...",
			"[B] Return to system information",
		)

	case stateDone:
		overlayContent = fmt.Sprintf(
			"%s\n\n%s\n\n%s\n\n%s\n\n%s",
			lipgloss.NewStyle().Bold(true).Render("Log Creation Completed"),
			"All diagnostics completed successfully.",
			fmt.Sprintf("Output file: %s", m.logFilePath),
			"Press ENTER to exit",
			"[B] Return to system information",
		)
	}

	// Рассчитываем размер и положение оверлея
	overlayWidth := m.width / 2
	if overlayWidth < 50 {
		overlayWidth = m.width - 10
	}

	overlay := overlayStyle.Width(overlayWidth).Render(overlayContent)

	// Создаем эффект наложения вручную
	return lipgloss.PlaceHorizontal(
		m.width,
		lipgloss.Center,
		lipgloss.PlaceVertical(
			m.height,
			lipgloss.Center,
			overlay,
		),
	)
}

// Вспомогательная функция для обрезки длинных строк
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Функция для получения максимального значения
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	// Проверяем, что программа запущена от имени root
	if os.Geteuid() != 0 {
		fmt.Println("Эта программа должна быть запущена с правами root. Используйте sudo или su.")
		os.Exit(1)
	}

	// Очищаем экран перед запуском для исключения артефактов отображения
	fmt.Print("\033[H\033[2J")

	p := tea.NewProgram(
		initialModel(),
		tea.WithAltScreen(),       // Используем альтернативный экран
		tea.WithMouseCellMotion(), // Поддержка мыши для лучшего взаимодействия
	)

	// Запускаем программу
	if _, err := p.Run(); err != nil {
		fmt.Println("Ошибка выполнения программы:", err)
		os.Exit(1)
	}

	// Очищаем экран при выходе
	fmt.Print("\033[H\033[2J")
}

// Функция для отрисовки тестовой таблицы SMPTE HD на весь экран
func drawSMPTETestPattern(width, height, timeRemaining int) string {
	var result strings.Builder

	// Используем всю высоту экрана (без строки статуса внизу)
	fullHeight := height - 1

	// Рассчитываем высоту каждой полосы
	// SMPTE HD тестовая таблица имеет 3 основные секции:
	// - Верхние 7 цветных полос (75% яркости) - 40% высоты
	// - Средние 7 цветных полос (100% яркости) - 30% высоты
	// - Нижняя секция с различными тестовыми элементами - 30% высоты

	upperHeight := int(float64(fullHeight) * 0.4)          // 40% для верхних полос
	middleHeight := int(float64(fullHeight) * 0.3)         // 30% для средних полос
	lowerHeight := fullHeight - upperHeight - middleHeight // Оставшаяся часть для нижних элементов

	// Создаем верхние 7 цветных полос (75% яркости)
	colors75 := []string{"#C0C0C0", "#C0C000", "#00C0C0", "#00C000", "#C000C0", "#C00000", "#0000C0"}
	colWidth := width / len(colors75)

	// Отрисовываем верхние полосы
	for row := 0; row < upperHeight; row++ {
		for _, color := range colors75 {
			result.WriteString(lipgloss.NewStyle().
				Background(lipgloss.Color(color)).
				Width(colWidth).
				Render(""))
		}
		result.WriteString("\n")
	}

	// Отрисовываем средние полосы (100% яркости)
	colors100 := []string{"#FFFFFF", "#FFFF00", "#00FFFF", "#00FF00", "#FF00FF", "#FF0000", "#0000FF"}
	for row := 0; row < middleHeight; row++ {
		for _, color := range colors100 {
			result.WriteString(lipgloss.NewStyle().
				Background(lipgloss.Color(color)).
				Width(colWidth).
				Render(""))
		}
		result.WriteString("\n")
	}

	// Отрисовываем нижние тестовые элементы
	lowerSection := [][]string{
		{"#0000C0", "#000000", "#0000C0", "#000000", "#0000C0", "#000000", "#0000C0"},
		{"#FFFFFF", "#000000", "#FFFFFF", "#000000", "#FFFFFF", "#000000", "#FFFFFF"},
	}

	// Разделяем нижнюю секцию на равные части для каждого типа паттерна
	patternHeight := lowerHeight / len(lowerSection)
	if patternHeight < 1 {
		patternHeight = 1
	}

	for i, pattern := range lowerSection {
		// Определяем сколько строк отрисовать для текущего паттерна
		rowsToDraw := patternHeight
		if i == len(lowerSection)-1 {
			// Последний паттерн получает все оставшиеся строки
			rowsToDraw = lowerHeight - (patternHeight * i)
		}

		for row := 0; row < rowsToDraw; row++ {
			for _, color := range pattern {
				result.WriteString(lipgloss.NewStyle().
					Background(lipgloss.Color(color)).
					Width(colWidth).
					Render(""))
			}
			result.WriteString("\n")
		}
	}

	// Добавляем информацию о необходимости нажать клавишу для продолжения (в нижней строке экрана)
	progressInfo := "Press any key to continue... | B to return to system info"

	return fmt.Sprintf(
		"%s%s",
		result.String(),
		lipgloss.NewStyle().
			Align(lipgloss.Center).
			Width(width).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#000000")).
			Bold(true).
			Render(progressInfo),
	)
}
