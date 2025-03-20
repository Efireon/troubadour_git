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
	Model  string
	Memory string
	Driver string
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
	if err == nil {
		info.Cores, _ = strconv.Atoi(strings.TrimSpace(string(physicalCoresOutput)))
	}

	// Если не удалось получить количество ядер, считаем уникальные physical id
	if info.Cores == 0 {
		physicalCoresCmd = exec.Command("sh", "-c", "cat /proc/cpuinfo | grep 'physical id' | sort -u | wc -l")
		physicalCoresOutput, err := physicalCoresCmd.Output()
		if err == nil {
			info.Cores, _ = strconv.Atoi(strings.TrimSpace(string(physicalCoresOutput)))
		}
	}

	// Получаем количество логических ядер
	threadsCmd := exec.Command("sh", "-c", "cat /proc/cpuinfo | grep processor | wc -l")
	threadsOutput, err := threadsCmd.Output()
	if err == nil {
		info.Threads, _ = strconv.Atoi(strings.TrimSpace(string(threadsOutput)))
	}

	// Получаем частоту
	freqCmd := exec.Command("sh", "-c", "lscpu | grep 'CPU MHz' | awk '{print $3}'")
	freqOutput, err := freqCmd.Output()
	if err == nil {
		freqMHz, _ := strconv.ParseFloat(strings.TrimSpace(string(freqOutput)), 64)
		info.Frequency = fmt.Sprintf("%.1f GHz", freqMHz/1000.0)
	} else {
		// Если не удалось получить из lscpu, пробуем получить из cpuinfo
		freqRegex := regexp.MustCompile(`cpu MHz\s*:\s*(.+)`)
		freq := freqRegex.FindSubmatch(cpuinfo)
		if len(freq) > 1 {
			freqMHz, _ := strconv.ParseFloat(strings.TrimSpace(string(freq[1])), 64)
			info.Frequency = fmt.Sprintf("%.1f GHz", freqMHz/1000.0)
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

		// Получаем модель для конкретного интерфейса
		// Сначала определяем путь к устройству через readlink
		devicePath, err := os.Readlink(filepath.Join(netDir, ifName))
		if err == nil {
			// Ищем информацию о производителе устройства
			vendorCmdStr := fmt.Sprintf("lspci | grep -i $(basename \"%s\" | cut -d: -f1) | head -1", devicePath)
			vendorCmd := exec.Command("sh", "-c", vendorCmdStr)
			vendorOutput, err := vendorCmd.Output()
			if err == nil && len(vendorOutput) > 0 {
				netInfo.Model = strings.TrimSpace(string(vendorOutput))
			} else {
				// Пробуем другой подход с ethtool
				ethtoolCmd := exec.Command("ethtool", "-i", ifName)
				ethtoolOutput, err := ethtoolCmd.Output()
				if err == nil {
					lines := strings.Split(string(ethtoolOutput), "\n")
					for _, line := range lines {
						if strings.HasPrefix(line, "driver:") {
							parts := strings.SplitN(line, ":", 2)
							if len(parts) > 1 {
								netInfo.Model = strings.TrimSpace(parts[1])
							}
						}
					}
				}
			}
		}

		// Если не удалось получить модель другими способами, пробуем через модуль
		if netInfo.Model == "" {
			moduleCmd := exec.Command("sh", "-c", fmt.Sprintf("basename $(readlink -f /sys/class/net/%s/device/driver/module 2>/dev/null) 2>/dev/null || echo 'Unknown'", ifName))
			moduleOutput, err := moduleCmd.Output()
			if err == nil {
				module := strings.TrimSpace(string(moduleOutput))
				if module != "Unknown" {
					netInfo.Model = fmt.Sprintf("Driver: %s", module)
				} else {
					netInfo.Model = "Network Interface"
				}
			} else {
				netInfo.Model = "Network Interface"
			}
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

		// Пробуем получить более подробную информацию с помощью различных инструментов

		// Пробуем nvidia-smi для NVIDIA карт
		nvidiaCmd := exec.Command("sh", "-c", "nvidia-smi --query-gpu=name,memory.total --format=csv,noheader")
		nvidiaOutput, err := nvidiaCmd.Output()
		if err == nil && len(nvidiaOutput) > 0 {
			parts := strings.Split(string(nvidiaOutput), ",")
			if len(parts) >= 2 {
				info.Model = strings.TrimSpace(parts[0])
				info.Memory = strings.TrimSpace(parts[1])

				// Получаем версию драйвера
				driverCmd := exec.Command("sh", "-c", "nvidia-smi --query-gpu=driver_version --format=csv,noheader")
				driverOutput, err := driverCmd.Output()
				if err == nil && len(driverOutput) > 0 {
					info.Driver = fmt.Sprintf("NVIDIA %s", strings.TrimSpace(string(driverOutput)))
				}
			}
		} else {
			// Пробуем для AMD карт
			amdCmd := exec.Command("sh", "-c", "glxinfo | grep 'OpenGL renderer'")
			amdOutput, err := amdCmd.Output()
			if err == nil && len(amdOutput) > 0 {
				rendererRegex := regexp.MustCompile(`OpenGL renderer string: (.+)`)
				match := rendererRegex.FindStringSubmatch(string(amdOutput))
				if len(match) > 1 {
					info.Model = match[1]

					// Получаем версию драйвера
					versionCmd := exec.Command("sh", "-c", "glxinfo | grep 'OpenGL version'")
					versionOutput, err := versionCmd.Output()
					if err == nil && len(versionOutput) > 0 {
						versionRegex := regexp.MustCompile(`OpenGL version string: (.+)`)
						match := versionRegex.FindStringSubmatch(string(versionOutput))
						if len(match) > 1 {
							info.Driver = match[1]
						}
					}
				}
			} else {
				// Проверяем Intel Graphics
				intelCmd := exec.Command("sh", "-c", "lspci | grep -i 'vga' | grep -i 'intel'")
				intelOutput, err := intelCmd.Output()
				if err == nil && len(intelOutput) > 0 {
					info.Model = "Intel Integrated Graphics"

					// Пытаемся получить версию драйвера
					driverCmd := exec.Command("sh", "-c", "glxinfo | grep 'OpenGL version'")
					driverOutput, err := driverCmd.Output()
					if err == nil && len(driverOutput) > 0 {
						info.Driver = string(driverOutput)
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

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
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

			// Каждые 2 секунды меняем цвет (красный -> зеленый -> синий -> тестовая таблица -> завершение)
			if elapsedSeconds >= 8 {
				// Завершаем тест после 8 секунд
				m.videoTestActive = false
				m.state = stateAskVideoOk
				return m, nil
			} else {
				// Меняем цвет каждые 2 секунды
				m.videoTestColor = (elapsedSeconds / 2) % 4
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
		// Серийный номер совпал, создаем логи
		m.state = stateCreateLogs
		m.showOverlay = true
		m.serialMatched = true
		return m, func() tea.Msg {
			return createLogFilesCmd(m.sysInfo, m.dmidecodeRaw, m.testPassed, true)
		}

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
		Margin(0).
		Width(m.width).
		Align(lipgloss.Center)

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#3C3C3C")).
		Width(m.width-4).
		Padding(0, 1)

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

	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#CDCDCD")).
		Padding(0, 2)

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

		switch m.videoTestColor {
		case 0:
			colorBg = "#FF0000"
			colorName = "RED"
		case 1:
			colorBg = "#00FF00"
			colorName = "GREEN"
		case 2:
			colorBg = "#0000FF"
			colorName = "BLUE"
		case 3:
			// Настроечная таблица SMPTE HD
			return drawSMPTETestPattern(m.width, m.height, 8-int(time.Since(m.videoTestStart).Seconds()))
		}

		testBg := lipgloss.NewStyle().
			Background(lipgloss.Color(colorBg)).
			Width(m.width).
			Height(m.height - 2)

		progressInfo := fmt.Sprintf(
			"Testing video... %s (%d/4) [%d sec remaining]",
			colorName,
			m.videoTestColor+1,
			8-int(time.Since(m.videoTestStart).Seconds()),
		)

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

	// Инициализация - показываем спиннер
	if m.state == stateInit {
		return borderStyle.Render(fmt.Sprintf(
			"%s\n\n%s\n\n%s",
			titleStyle.Render("TROUBADOUR"),
			lipgloss.NewStyle().Align(lipgloss.Center).Width(m.width-6).Render("Collecting system information..."),
			lipgloss.NewStyle().Align(lipgloss.Center).Width(m.width-6).Render(m.spinner.View()),
		))
	}

	// Если произошла ошибка
	if m.err != nil {
		return borderStyle.Render(fmt.Sprintf(
			"%s\n\n%s\n\n%s",
			titleStyle.Render("TROUBADOUR"),
			errorStyle.Render(fmt.Sprintf("ERROR: %v", m.err)),
			footerStyle.Render("Press any key to exit"),
		))
	}

	// Базовый вывод информации о системе
	mainContent := strings.Builder{}

	// Расставляем информацию в две колонки
	// Колонка 1: Процессор и Сеть
	// Колонка 2: Память, GPU и Хранение

	leftColWidth := (m.width - 12) / 2
	rightColWidth := (m.width - 12) / 2

	// Формируем левую колонку
	leftCol := strings.Builder{}

	// Процессор
	cpuContent := strings.Builder{}
	cpuContent.WriteString(fmt.Sprintf("Model: %s\n", m.sysInfo.Processor.Model))
	cpuContent.WriteString(fmt.Sprintf("Cores: %d (Threads: %d)\n", m.sysInfo.Processor.Cores, m.sysInfo.Processor.Threads))
	cpuContent.WriteString(fmt.Sprintf("Freq: %s\n", m.sysInfo.Processor.Frequency))

	cacheStr := ""
	for level, size := range m.sysInfo.Processor.Cache {
		if cacheStr != "" {
			cacheStr += " "
		}
		cacheStr += fmt.Sprintf("%s:%s", level, size)
	}
	cpuContent.WriteString(fmt.Sprintf("Cache: %s", cacheStr))

	leftCol.WriteString(sectionStyle.Width(leftColWidth).Render(
		fmt.Sprintf("%s\n%s",
			sectionTitleStyle.Render("─── PROCESSOR ───"),
			cpuContent.String(),
		),
	))

	// Сеть
	netContent := strings.Builder{}
	for _, net := range m.sysInfo.Network {
		netContent.WriteString(fmt.Sprintf("%s: %s\n", net.Interface, net.Model))
		netContent.WriteString(fmt.Sprintf("MAC: %s\n\n", net.MAC))
	}

	leftCol.WriteString("\n\n")
	leftCol.WriteString(sectionStyle.Width(leftColWidth).Render(
		fmt.Sprintf("%s\n%s",
			sectionTitleStyle.Render("─── NETWORK ───"),
			netContent.String(),
		),
	))

	// Формируем правую колонку
	rightCol := strings.Builder{}

	// Память
	memContent := strings.Builder{}
	memContent.WriteString(fmt.Sprintf("Total: %s\n\n", m.sysInfo.Memory.Total))

	for _, slot := range m.sysInfo.Memory.Slots {
		memContent.WriteString(fmt.Sprintf("Slot %s: %s %s\n",
			slot.ID, slot.Manufacturer, slot.Size))
		if slot.Speed != "" || slot.Type != "" {
			memContent.WriteString(fmt.Sprintf("         %s %s\n\n",
				slot.Speed, slot.Type))
		} else {
			memContent.WriteString("\n")
		}
	}

	rightCol.WriteString(sectionStyle.Width(rightColWidth).Render(
		fmt.Sprintf("%s\n%s",
			sectionTitleStyle.Render("─── MEMORY ───"),
			memContent.String(),
		),
	))

	// GPU
	gpuContent := strings.Builder{}
	gpuContent.WriteString(fmt.Sprintf("Model: %s\n", m.sysInfo.GPU.Model))
	if m.sysInfo.GPU.Memory != "" {
		gpuContent.WriteString(fmt.Sprintf("Memory: %s\n", m.sysInfo.GPU.Memory))
	}
	if m.sysInfo.GPU.Driver != "" {
		gpuContent.WriteString(fmt.Sprintf("Driver: %s\n", m.sysInfo.GPU.Driver))
	}

	rightCol.WriteString("\n\n")
	rightCol.WriteString(sectionStyle.Width(rightColWidth).Render(
		fmt.Sprintf("%s\n%s",
			sectionTitleStyle.Render("─── GPU ───"),
			gpuContent.String(),
		),
	))

	// Хранение
	storageContent := strings.Builder{}
	for _, storage := range m.sysInfo.Storage {
		storageContent.WriteString(fmt.Sprintf("%s: %s %s\n",
			storage.Type, storage.Model, storage.Size))
		if storage.Label != "" {
			storageContent.WriteString(fmt.Sprintf("Label: %s\n", storage.Label))
		}
		storageContent.WriteString("\n")
	}

	rightCol.WriteString("\n\n")
	rightCol.WriteString(sectionStyle.Width(rightColWidth).Render(
		fmt.Sprintf("%s\n%s",
			sectionTitleStyle.Render("─── STORAGE ───"),
			storageContent.String(),
		),
	))

	// Формируем общий вывод, размещая колонки рядом
	leftRows := strings.Split(leftCol.String(), "\n")
	rightRows := strings.Split(rightCol.String(), "\n")

	maxRows := len(leftRows)
	if len(rightRows) > maxRows {
		maxRows = len(rightRows)
	}

	// Добавляем пустые строки, если нужно
	for len(leftRows) < maxRows {
		leftRows = append(leftRows, "")
	}
	for len(rightRows) < maxRows {
		rightRows = append(rightRows, "")
	}

	for i := 0; i < maxRows; i++ {
		mainContent.WriteString(fmt.Sprintf("%-*s  %-*s\n",
			leftColWidth, leftRows[i], rightColWidth, rightRows[i]))
	}

	baseView := fmt.Sprintf(
		"%s\n%s\n\n%s",
		titleStyle.Render("TROUBADOUR"),
		borderStyle.Render(mainContent.String()),
		footerStyle.Render("Press ENTER to continue to video test..."),
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
			"%s\n\n%s\n\n%s",
			lipgloss.NewStyle().Bold(true).Render("Video Test Completed"),
			"Did all test patterns display correctly?",
			"[Y] Yes (default)   [n] No, run test again",
		)

	case stateAskSerial:
		overlayContent = fmt.Sprintf(
			"%s\n\n%s\n\n%s",
			lipgloss.NewStyle().Bold(true).Render("Serial Number Verification"),
			fmt.Sprintf("System Serial Number: %s", m.sysInfo.SerialNumber),
			fmt.Sprintf("Please enter Serial Number: %s", m.textInput.View()),
		)

	case stateSerialError:
		errorBox := errorStyle.Width(45).Render(fmt.Sprintf(
			"Serial numbers DO NOT match!\n\nSystem: %s\nEntered: %s\n\n[R] Restart system\n[E] Shutdown system\n[ENTER] Try again",
			m.sysInfo.SerialNumber, m.userSerial,
		))

		overlayContent = fmt.Sprintf(
			"%s\n\n%s",
			lipgloss.NewStyle().Bold(true).Render("Serial Number Verification Failed"),
			errorBox,
		)

	case stateCreateLogs:
		overlayContent = fmt.Sprintf(
			"%s\n\n%s\n\n%s\n%s\n%s\n%s",
			lipgloss.NewStyle().Bold(true).Render("Log Creation"),
			"Creating system logs...",
			"■ Hardware information collected",
			"■ System verification completed",
			"■ dmidecode data parsed",
			"□ Writing log file...",
		)

	case stateDone:
		overlayContent = fmt.Sprintf(
			"%s\n\n%s\n\n%s\n\n%s",
			lipgloss.NewStyle().Bold(true).Render("Log Creation Completed"),
			"All diagnostics completed successfully.",
			fmt.Sprintf("Output file: %s", m.logFilePath),
			"Press ENTER to exit",
		)
	}

	// Рассчитываем размер и положение оверлея
	overlayWidth := m.width / 2
	if overlayWidth < 50 {
		overlayWidth = m.width - 10
	}

	overlay := overlayStyle.Width(overlayWidth).Render(overlayContent)

	// Собираем финальный вид с оверлеем
	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		overlay,
		lipgloss.WithBackground(baseView),
	)

	// Если произошла ошибка
	if m.err != nil {
		return borderStyle.Render(fmt.Sprintf(
			"%s\n\n%s\n\n%s",
			titleStyle.Render("TROUBADOUR"),
			errorStyle.Render(fmt.Sprintf("ERROR: %v", m.err)),
			footerStyle.Render("Press any key to exit"),
		))
	}

	return "Loading..."
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

// Функция для отрисовки тестовой таблицы SMPTE HD
func drawSMPTETestPattern(width, height, timeRemaining int) string {
	var result strings.Builder

	// Рассчитываем высоту каждой полосы
	// SMPTE HD тестовая таблица имеет 3 основные секции:
	// - Верхние 7 цветных полос (75% яркости)
	// - Средние 7 цветных полос (100% яркости)
	// - Нижняя секция с различными тестовыми элементами

	rowHeight := (height - 2) / 10

	// Создаем верхние 7 цветных полос (75% яркости)
	colors75 := []string{"#C0C0C0", "#C0C000", "#00C0C0", "#00C000", "#C000C0", "#C00000", "#0000C0"}
	colWidth := width / len(colors75)

	// Отрисовываем верхние полосы
	for row := 0; row < rowHeight*4; row++ {
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
	for row := 0; row < rowHeight*3; row++ {
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

	for row := 0; row < rowHeight*2; row++ {
		sectionIdx := row / rowHeight
		if sectionIdx >= len(lowerSection) {
			sectionIdx = len(lowerSection) - 1
		}

		for _, color := range lowerSection[sectionIdx] {
			result.WriteString(lipgloss.NewStyle().
				Background(lipgloss.Color(color)).
				Width(colWidth).
				Render(""))
		}
		result.WriteString("\n")
	}

	// Добавляем информацию о прогрессе теста
	progressInfo := fmt.Sprintf(
		"Testing video... SMPTE HD Test Pattern (4/4) [%d sec remaining]",
		timeRemaining,
	)

	return fmt.Sprintf(
		"%s%s",
		result.String(),
		lipgloss.NewStyle().
			Align(lipgloss.Center).
			Width(width).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#000000")).
			Render(progressInfo),
	)
}
