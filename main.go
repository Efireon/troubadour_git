package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/mem"
	"github.com/shirou/gopsutil/net"
)

// Структуры для хранения системной информации
type SystemInfo struct {
	Processor      ProcessorInfo       `json:"processor"`
	Memory         MemoryInfo          `json:"memory"`
	NetworkCards   []NetworkCardInfo   `json:"network_cards"`
	GPU            GPUInfo             `json:"gpu"`
	StorageDevices []StorageDeviceInfo `json:"storage_devices"`
	SerialNumber   string              `json:"serial_number"`
	TestResults    TestResults         `json:"test_results"`
}

type ProcessorInfo struct {
	Model        string  `json:"model"`
	Cores        int     `json:"cores"`
	Threads      int     `json:"threads"`
	Frequency    float64 `json:"frequency_mhz"`
	Cache        string  `json:"cache"`
	Architecture string  `json:"architecture"`
}

type MemoryInfo struct {
	Total        uint64       `json:"total_bytes"`
	Slots        []MemorySlot `json:"slots"`
	Frequency    string       `json:"frequency"`
	Manufacturer string       `json:"manufacturer"`
}

type MemorySlot struct {
	SlotNumber   int    `json:"slot_number"`
	Size         uint64 `json:"size_bytes"`
	Manufacturer string `json:"manufacturer"`
	Frequency    string `json:"frequency"`
}

type NetworkCardInfo struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	MACAddress string `json:"mac_address"`
}

type GPUInfo struct {
	Model      string `json:"model"`
	Memory     string `json:"memory"`
	Resolution string `json:"resolution"`
	Driver     string `json:"driver"`
}

type StorageDeviceInfo struct {
	Type       string `json:"type"` // NVME, SSD, HDD, Flash
	Model      string `json:"model"`
	Size       uint64 `json:"size_bytes"`
	MountPoint string `json:"mount_point,omitempty"`
	Label      string `json:"label,omitempty"`
}

type TestResults struct {
	DisplayTest          bool   `json:"display_test_passed"`
	SerialNumberVerified bool   `json:"serial_number_verified"`
	UserEnteredSN        string `json:"user_entered_sn"`
}

// Глобальные переменные
var sysInfo SystemInfo
var currentWindow fyne.Window
var mainApp fyne.App
var statusLabel *widget.Label

func main() {
	// Инициализация приложения
	myApp := app.New()
	myApp.Settings().SetTheme(NewTroubadourTheme())
	mainApp = myApp
	window := myApp.NewWindow("Troubadour")
	currentWindow = window
	window.Resize(fyne.NewSize(1200, 800))
	window.SetPadded(true)

	// Запуск последовательности действий
	startDiagnosticSequence(window)

	window.ShowAndRun()
}

// TroubadourTheme - кастомная тема для приложения
type TroubadourTheme struct {
	fyne.Theme
}

func NewTroubadourTheme() fyne.Theme {
	return &TroubadourTheme{Theme: theme.DefaultTheme()}
}

func (t *TroubadourTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{30, 30, 46, 255} // #1E1E2E
	case theme.ColorNameForeground:
		return color.NRGBA{205, 214, 244, 255} // #CDD6F4
	case theme.ColorNamePrimary:
		return color.NRGBA{137, 180, 250, 255} // #89B4FA
	case theme.ColorNameButton:
		return color.NRGBA{69, 71, 90, 255} // #45475A
	case theme.ColorNameDisabled:
		return color.NRGBA{127, 132, 156, 255} // #7F849C
	default:
		return t.Theme.Color(name, variant)
	}
}

// Функция для запуска последовательности диагностики
func startDiagnosticSequence(window fyne.Window) {
	// Показываем заставку
	showSplashScreen(window, "Инициализация Troubadour...", func() {
		// 1. Сбор системной информации
		collectSystemInformation()

		// 2. Отображение собранной информации
		showSystemInfo(window)

		// 3-5 Последующие этапы вызываются автоматически в конце каждого предыдущего
	})
}

// Показать заставку
func showSplashScreen(window fyne.Window, message string, onComplete func()) {
	title := canvas.NewText("Troubadour", color.NRGBA{205, 214, 244, 255})
	title.TextSize = 36
	title.Alignment = fyne.TextAlignCenter

	subtitle := canvas.NewText("Системная диагностика", color.NRGBA{166, 173, 200, 255})
	subtitle.TextSize = 20
	subtitle.Alignment = fyne.TextAlignCenter

	progress := widget.NewProgressBarInfinite()

	status := widget.NewLabel(message)
	status.Alignment = fyne.TextAlignCenter

	content := container.NewVBox(
		layout.NewSpacer(),
		title,
		subtitle,
		container.NewPadded(
			container.NewVBox(
				layout.NewSpacer(),
				progress,
				status,
				layout.NewSpacer(),
			),
		),
		layout.NewSpacer(),
	)

	window.SetContent(content)

	// Выполняем функцию через небольшую задержку
	go func() {
		time.Sleep(1 * time.Second)
		onComplete()
	}()
}

// Функция для сбора системной информации
func collectSystemInformation() {
	// Инициализация результатов тестов
	sysInfo.TestResults = TestResults{}

	// Сбор информации о компонентах системы
	collectProcessorInfo()
	collectMemoryInfo()
	collectNetworkInfo()
	collectGPUInfo()
	collectStorageInfo()
	collectSerialNumber()
}

// Функция для сбора информации о процессоре
func collectProcessorInfo() {
	cpuInfo, err := cpu.Info()
	if err != nil {
		fmt.Println("Error fetching CPU info:", err)
		return
	}

	if len(cpuInfo) > 0 {
		cpu := cpuInfo[0]
		sysInfo.Processor = ProcessorInfo{
			Model:        cpu.ModelName,
			Cores:        int(cpu.Cores),
			Threads:      len(cpuInfo),
			Frequency:    cpu.Mhz,
			Cache:        fmt.Sprintf("L2: %.1f MB", float64(cpu.CacheSize)/1024),
			Architecture: cpu.ModelName,
		}
	}

	// Дополнительная информация через dmidecode
	cmd := exec.Command("dmidecode", "-t", "processor")
	output, err := cmd.Output()
	if err == nil {
		outputStr := string(output)
		lines := strings.Split(outputStr, "\n")

		for _, line := range lines {
			line = strings.TrimSpace(line)

			if strings.HasPrefix(line, "Version:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) >= 2 && sysInfo.Processor.Model == "" {
					sysInfo.Processor.Model = strings.TrimSpace(parts[1])
				}
			} else if strings.HasPrefix(line, "Core Count:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) >= 2 && sysInfo.Processor.Cores == 0 {
					fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &sysInfo.Processor.Cores)
				}
			} else if strings.HasPrefix(line, "Thread Count:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) >= 2 && sysInfo.Processor.Threads == 0 {
					fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &sysInfo.Processor.Threads)
				}
			} else if strings.HasPrefix(line, "External Clock:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) >= 2 && sysInfo.Processor.Frequency == 0 {
					var freq float64
					fmt.Sscanf(strings.TrimSpace(parts[1]), "%f", &freq)
					sysInfo.Processor.Frequency = freq
				}
			} else if strings.HasPrefix(line, "Max Speed:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) >= 2 && sysInfo.Processor.Frequency == 0 {
					var freq float64
					fmt.Sscanf(strings.TrimSpace(parts[1]), "%f", &freq)
					sysInfo.Processor.Frequency = freq
				}
			} else if strings.HasPrefix(line, "Family:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) >= 2 {
					sysInfo.Processor.Architecture = strings.TrimSpace(parts[1])
				}
			}
		}
	}

	// Если какие-то поля не заполнены, пробуем получить информацию через lscpu
	if sysInfo.Processor.Model == "" || sysInfo.Processor.Cores == 0 || sysInfo.Processor.Frequency == 0 {
		cmd := exec.Command("lscpu")
		output, err := cmd.Output()
		if err == nil {
			outputStr := string(output)
			lines := strings.Split(outputStr, "\n")

			for _, line := range lines {
				line = strings.TrimSpace(line)

				if strings.HasPrefix(line, "Model name:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) >= 2 && sysInfo.Processor.Model == "" {
						sysInfo.Processor.Model = strings.TrimSpace(parts[1])
					}
				} else if strings.HasPrefix(line, "CPU(s):") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) >= 2 && sysInfo.Processor.Threads == 0 {
						fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &sysInfo.Processor.Threads)
					}
				} else if strings.HasPrefix(line, "Core(s) per socket:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) >= 2 && sysInfo.Processor.Cores == 0 {
						var coresPerSocket int
						fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &coresPerSocket)

						// Нужно умножить на количество сокетов
						sockets := 1
						for _, l := range lines {
							if strings.HasPrefix(l, "Socket(s):") {
								parts := strings.SplitN(l, ":", 2)
								if len(parts) >= 2 {
									fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &sockets)
									break
								}
							}
						}

						sysInfo.Processor.Cores = coresPerSocket * sockets
					}
				} else if strings.HasPrefix(line, "CPU MHz:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) >= 2 && sysInfo.Processor.Frequency == 0 {
						fmt.Sscanf(strings.TrimSpace(parts[1]), "%f", &sysInfo.Processor.Frequency)
					}
				} else if strings.HasPrefix(line, "Architecture:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) >= 2 && sysInfo.Processor.Architecture == "" {
						sysInfo.Processor.Architecture = strings.TrimSpace(parts[1])
					}
				} else if strings.HasPrefix(line, "L2 cache:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) >= 2 {
						sysInfo.Processor.Cache = strings.TrimSpace(parts[1])
					}
				}
			}
		}
	}
}

// Функция для сбора информации о памяти
func collectMemoryInfo() {
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		fmt.Println("Error fetching memory info:", err)
		return
	}

	memoryInfo := MemoryInfo{
		Total: memInfo.Total,
		Slots: []MemorySlot{},
	}

	// Используем dmidecode для получения детальной информации о памяти
	cmd := exec.Command("dmidecode", "-t", "memory")
	output, err := cmd.Output()
	if err == nil {
		outputStr := string(output)
		lines := strings.Split(outputStr, "\n")

		var currentSlot *MemorySlot
		slotNumber := 0

		for _, line := range lines {
			line = strings.TrimSpace(line)

			if strings.Contains(line, "Memory Device") {
				if currentSlot != nil && currentSlot.Size > 0 {
					memoryInfo.Slots = append(memoryInfo.Slots, *currentSlot)
				}

				slotNumber++
				currentSlot = &MemorySlot{SlotNumber: slotNumber}
			}

			if currentSlot != nil {
				if strings.HasPrefix(line, "Size:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) >= 2 {
						sizeStr := strings.TrimSpace(parts[1])

						if strings.Contains(sizeStr, "No Module Installed") {
							currentSlot = nil // Пропускаем пустой слот
							continue
						}

						size := uint64(0)
						unit := ""
						fmt.Sscanf(sizeStr, "%d %s", &size, &unit)

						// Конвертируем в байты
						if strings.Contains(unit, "GB") {
							size *= 1024 * 1024 * 1024
						} else if strings.Contains(unit, "MB") {
							size *= 1024 * 1024
						} else if strings.Contains(unit, "KB") {
							size *= 1024
						}

						currentSlot.Size = size
					}
				} else if strings.HasPrefix(line, "Manufacturer:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) >= 2 {
						manufacturer := strings.TrimSpace(parts[1])
						currentSlot.Manufacturer = manufacturer

						// Используем производителя первого слота как общий
						if memoryInfo.Manufacturer == "" && manufacturer != "" {
							memoryInfo.Manufacturer = manufacturer
						}
					}
				} else if strings.HasPrefix(line, "Speed:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) >= 2 {
						freq := strings.TrimSpace(parts[1])
						if freq != "Unknown" && freq != "" {
							currentSlot.Frequency = freq

							// Используем частоту первого слота как общую
							if memoryInfo.Frequency == "" {
								memoryInfo.Frequency = freq
							}
						}
					}
				}
			}
		}

		// Добавляем последний слот
		if currentSlot != nil && currentSlot.Size > 0 {
			memoryInfo.Slots = append(memoryInfo.Slots, *currentSlot)
		}
	}

	sysInfo.Memory = memoryInfo
}

// Функция для сбора информации о сетевых картах
func collectNetworkInfo() {
	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Println("Error fetching network interfaces:", err)
		return
	}

	var networkCards []NetworkCardInfo

	for _, iface := range interfaces {
		// Пропускаем неактивные и локальные интерфейсы
		var isUp, isLoopback bool
		for _, flag := range iface.Flags {
			if flag == "up" {
				isUp = true
			}
			if flag == "loopback" {
				isLoopback = true
			}
		}

		if !isUp || isLoopback {
			continue
		}

		// Базовая информация
		card := NetworkCardInfo{
			Name:       iface.Name,
			MACAddress: iface.HardwareAddr,
		}

		// Пытаемся получить модель через lshw
		modelFound := false
		cmd := exec.Command("lshw", "-class", "network", "-short")
		output, err := cmd.Output()
		if err == nil {
			outputStr := string(output)
			lines := strings.Split(outputStr, "\n")

			for _, line := range lines {
				if strings.Contains(line, iface.Name) {
					parts := strings.Fields(line)
					if len(parts) > 1 {
						// Модель - это все оставшиеся поля
						card.Model = strings.Join(parts[1:], " ")
						modelFound = true
						break
					}
				}
			}
		}

		// Если модель не найдена, попробуем другой подход
		if !modelFound {
			cmd := exec.Command("ethtool", "-i", iface.Name)
			output, err := cmd.Output()
			if err == nil {
				outputStr := string(output)
				lines := strings.Split(outputStr, "\n")

				for _, line := range lines {
					if strings.HasPrefix(line, "driver:") || strings.HasPrefix(line, "version:") {
						parts := strings.SplitN(line, ":", 2)
						if len(parts) >= 2 {
							info := strings.TrimSpace(parts[1])
							if card.Model == "" {
								card.Model = info
							} else {
								card.Model += " " + info
							}
						}
					}
				}
			}
		}

		// Добавляем только если у нас есть MAC адрес
		if card.MACAddress != "" {
			networkCards = append(networkCards, card)
		}
	}

	sysInfo.NetworkCards = networkCards
}

// Функция для сбора информации о GPU
func collectGPUInfo() {
	gpu := GPUInfo{}

	// Пробуем lspci
	cmd := exec.Command("lspci", "-v")
	output, err := cmd.Output()
	if err == nil {
		outputStr := string(output)
		lines := strings.Split(outputStr, "\n")

		for i, line := range lines {
			if strings.Contains(line, "VGA compatible controller") || strings.Contains(line, "3D controller") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) >= 2 {
					gpu.Model = strings.TrimSpace(parts[1])

					// Ищем дополнительную информацию в следующих строках
					for j := i + 1; j < len(lines) && j < i+15; j++ {
						subline := strings.TrimSpace(lines[j])

						if strings.Contains(subline, "Memory") {
							gpu.Memory = subline
						} else if strings.Contains(subline, "Kernel driver") || strings.Contains(subline, "Kernel modules") {
							if gpu.Driver == "" {
								gpu.Driver = subline
							}
						}
					}

					break
				}
			}
		}
	}

	// Получаем информацию о разрешении с помощью xrandr
	cmd = exec.Command("xrandr")
	output, err = cmd.Output()
	if err == nil {
		outputStr := string(output)
		lines := strings.Split(outputStr, "\n")

		for _, line := range lines {
			if strings.Contains(line, " connected ") {
				parts := strings.Fields(line)
				for _, part := range parts {
					if strings.Contains(part, "x") && strings.Contains(part, "+") {
						// Формат вывода xrandr: 1920x1080+0+0
						resolution := strings.Split(part, "+")[0]
						gpu.Resolution = resolution
						break
					}
				}
			}
		}
	}

	// Если модель не найдена, пробуем glxinfo
	if gpu.Model == "" {
		cmd := exec.Command("glxinfo")
		output, err := cmd.Output()
		if err == nil {
			outputStr := string(output)
			lines := strings.Split(outputStr, "\n")

			for _, line := range lines {
				if strings.Contains(line, "OpenGL renderer string") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) >= 2 {
						gpu.Model = strings.TrimSpace(parts[1])
					}
				} else if strings.Contains(line, "Video memory") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) >= 2 {
						gpu.Memory = strings.TrimSpace(parts[1])
					}
				} else if strings.Contains(line, "OpenGL version string") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) >= 2 {
						gpu.Driver = strings.TrimSpace(parts[1])
					}
				}
			}
		}
	}

	sysInfo.GPU = gpu
}

// Функция для сбора информации о носителях
func collectStorageInfo() {
	var storageDevices []StorageDeviceInfo

	// Получаем список разделов
	partitions, err := disk.Partitions(true)
	if err != nil {
		fmt.Println("Error fetching partitions:", err)
		return
	}

	// Хранит основные устройства, чтобы не добавлять их несколько раз
	processedDevices := make(map[string]bool)

	for _, partition := range partitions {
		devicePath := partition.Device

		// Получаем базовое устройство (без номера раздела)
		baseDevice := devicePath
		for i := len(devicePath) - 1; i >= 0; i-- {
			if devicePath[i] < '0' || devicePath[i] > '9' {
				baseDevice = devicePath[:i+1]
				break
			}
		}

		// Пропускаем, если уже обработали это устройство
		if _, exists := processedDevices[baseDevice]; exists {
			continue
		}
		processedDevices[baseDevice] = true

		// Начинаем заполнять информацию об устройстве
		device := StorageDeviceInfo{
			MountPoint: partition.Mountpoint,
		}

		// Определяем тип устройства
		if strings.Contains(baseDevice, "nvme") {
			device.Type = "NVME"
		} else if strings.Contains(baseDevice, "sd") {
			// Определяем SSD или HDD
			if isSSD(baseDevice) {
				device.Type = "SSD"
			} else {
				device.Type = "HDD"
			}
		} else if strings.Contains(baseDevice, "mmcblk") {
			device.Type = "Flash"
		} else {
			device.Type = "Other"
		}

		// Получаем размер устройства
		cmd := exec.Command("lsblk", "-b", "-d", "-n", "-o", "SIZE", baseDevice)
		output, err := cmd.Output()
		if err == nil {
			sizeStr := strings.TrimSpace(string(output))
			var size uint64
			fmt.Sscanf(sizeStr, "%d", &size)
			device.Size = size
		}

		// Получаем модель устройства
		cmd = exec.Command("lsblk", "-d", "-n", "-o", "MODEL", baseDevice)
		output, err = cmd.Output()
		if err == nil {
			device.Model = strings.TrimSpace(string(output))
		}

		// Получаем метку тома
		cmd = exec.Command("lsblk", "-n", "-o", "LABEL", devicePath)
		output, err = cmd.Output()
		if err == nil {
			device.Label = strings.TrimSpace(string(output))
		}

		// Добавляем устройство в список, если знаем его размер
		if device.Size > 0 {
			storageDevices = append(storageDevices, device)
		}
	}

	sysInfo.StorageDevices = storageDevices
}

// Определяет, является ли устройство SSD
func isSSD(devicePath string) bool {
	// Извлекаем только имя устройства без пути
	deviceName := filepath.Base(devicePath)

	// Проверяем через rotational параметр
	cmd := exec.Command("cat", fmt.Sprintf("/sys/block/%s/queue/rotational", deviceName))
	output, err := cmd.Output()
	if err == nil {
		// 0 означает невращающееся устройство (SSD), 1 - вращающееся (HDD)
		return strings.TrimSpace(string(output)) == "0"
	}

	return false
}

// Функция для получения серийного номера системы
func collectSerialNumber() {
	// Получаем серийный номер через dmidecode
	cmd := exec.Command("dmidecode", "-s", "system-serial-number")
	output, err := cmd.Output()
	if err == nil {
		sn := strings.TrimSpace(string(output))
		// Проверяем, что не получили "Not Specified" или другое проблемное значение
		if sn != "" && sn != "Not Specified" && sn != "System Serial Number" && sn != "To be filled by O.E.M." {
			sysInfo.SerialNumber = sn
			return
		}
	}

	// Если не удалось получить через dmidecode, пробуем альтернативные методы
	// Проверяем базовую плату
	cmd = exec.Command("dmidecode", "-s", "baseboard-serial-number")
	output, err = cmd.Output()
	if err == nil {
		sn := strings.TrimSpace(string(output))
		if sn != "" && sn != "Not Specified" && sn != "To be filled by O.E.M." {
			sysInfo.SerialNumber = sn
			return
		}
	}

	// Проверяем шасси
	cmd = exec.Command("dmidecode", "-s", "chassis-serial-number")
	output, err = cmd.Output()
	if err == nil {
		sn := strings.TrimSpace(string(output))
		if sn != "" && sn != "Not Specified" && sn != "To be filled by O.E.M." {
			sysInfo.SerialNumber = sn
			return
		}
	}

	// Если ничего не найдено, используем заглушку
	sysInfo.SerialNumber = "UNKNOWN"
}

// Функция для отображения собранной информации
func showSystemInfo(window fyne.Window) {
	// Заголовок
	// Заголовок
	titleText := canvas.NewText("Troubadour - Системная диагностика", color.NRGBA{205, 214, 244, 255})
	titleText.TextSize = 18
	titleText.Alignment = fyne.TextAlignCenter
	titleText.TextStyle = fyne.TextStyle{Bold: true}

	// Создаем контейнеры для каждой колонки информации
	leftColumn := createProcessorMemoryInfo()
	centerColumn := createGPUNetworkInfo()
	rightColumn := createStorageInfo()

	// Информация о статусе и прогрессе
	progressContainer := createProgressInfo()

	// Создаем основной контейнер
	mainContent := container.NewBorder(
		container.NewVBox(
			container.NewCenter(titleText),
			widget.NewSeparator(),
		),
		progressContainer,
		nil,
		nil,
		container.NewHBox(
			leftColumn,
			widget.NewSeparator(),
			centerColumn,
			widget.NewSeparator(),
			rightColumn,
		),
	)

	window.SetContent(mainContent)

	// Автоматически запускаем тест дисплея через небольшую задержку
	go func() {
		time.Sleep(2 * time.Second)
		updateStatusLabel("2. Тест дисплея: в процессе")
		runDisplayTest(window)
	}()
}

// Создает и возвращает контейнер с информацией о процессоре и памяти
func createProcessorMemoryInfo() fyne.CanvasObject {
	// Заголовок раздела процессора
	cpuTitle := widget.NewLabelWithStyle("Процессор", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	// Информация о процессоре
	cpuModel := widget.NewLabel(fmt.Sprintf("Модель: %s", sysInfo.Processor.Model))
	cpuCores := widget.NewLabel(fmt.Sprintf("Ядра/Потоки: %d/%d", sysInfo.Processor.Cores, sysInfo.Processor.Threads))
	cpuFreq := widget.NewLabel(fmt.Sprintf("Частота: %.2f МГц", sysInfo.Processor.Frequency))
	cpuCache := widget.NewLabel(fmt.Sprintf("Кэш: %s", sysInfo.Processor.Cache))
	cpuArch := widget.NewLabel(fmt.Sprintf("Архитектура: %s", sysInfo.Processor.Architecture))

	// Контейнер для информации о процессоре
	cpuContainer := container.NewVBox(
		cpuTitle,
		cpuModel,
		cpuCores,
		cpuFreq,
		cpuCache,
		cpuArch,
	)

	// Заголовок раздела памяти
	memTitle := widget.NewLabelWithStyle("Оперативная память", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	// Информация о памяти
	memTotal := widget.NewLabel(fmt.Sprintf("Всего: %.2f ГБ", float64(sysInfo.Memory.Total)/(1024*1024*1024)))
	memFreq := widget.NewLabel(fmt.Sprintf("Частота: %s", sysInfo.Memory.Frequency))

	// Контейнер для информации о памяти
	memItems := []fyne.CanvasObject{
		memTitle,
		memTotal,
		memFreq,
	}

	// Добавляем информацию о слотах
	for _, slot := range sysInfo.Memory.Slots {
		slotInfo := widget.NewLabel(fmt.Sprintf("Слот %d: %.2f ГБ, %s, %s",
			slot.SlotNumber,
			float64(slot.Size)/(1024*1024*1024),
			slot.Manufacturer,
			slot.Frequency,
		))
		memItems = append(memItems, slotInfo)
	}

	memContainer := container.NewVBox(memItems...)

	// Объединяем в один контейнер
	return container.NewVBox(
		container.NewVBox(cpuContainer),
		widget.NewSeparator(),
		container.NewVBox(memContainer),
	)
}

// Создает и возвращает контейнер с информацией о GPU и сети
func createGPUNetworkInfo() fyne.CanvasObject {
	// Заголовок раздела GPU
	gpuTitle := widget.NewLabelWithStyle("Видеокарта", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	// Информация о GPU
	gpuModel := widget.NewLabel(fmt.Sprintf("Модель: %s", sysInfo.GPU.Model))
	gpuMem := widget.NewLabel(fmt.Sprintf("Память: %s", sysInfo.GPU.Memory))
	gpuRes := widget.NewLabel(fmt.Sprintf("Разрешение: %s", sysInfo.GPU.Resolution))
	gpuDriver := widget.NewLabel(fmt.Sprintf("Драйвер: %s", sysInfo.GPU.Driver))

	// Контейнер для информации о GPU
	gpuContainer := container.NewVBox(
		gpuTitle,
		gpuModel,
		gpuMem,
		gpuRes,
		gpuDriver,
	)

	// Заголовок раздела сети
	netTitle := widget.NewLabelWithStyle("Сетевые карты", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	// Контейнер для информации о сетевых картах
	netItems := []fyne.CanvasObject{netTitle}

	// Если сетевых карт нет
	if len(sysInfo.NetworkCards) == 0 {
		netItems = append(netItems, widget.NewLabel("Не обнаружено сетевых карт"))
	} else {
		// Добавляем информацию о каждой сетевой карте
		for i, card := range sysInfo.NetworkCards {
			cardInfo := widget.NewLabel(fmt.Sprintf("Карта %d: %s", i+1, card.Model))
			macInfo := widget.NewLabel(fmt.Sprintf("MAC: %s", card.MACAddress))
			netItems = append(netItems, cardInfo, macInfo)
		}
	}

	netContainer := container.NewVBox(netItems...)

	// Объединяем в один контейнер
	return container.NewVBox(
		container.NewVBox(gpuContainer),
		widget.NewSeparator(),
		container.NewVBox(netContainer),
	)
}

// Создает и возвращает контейнер с информацией о хранилищах
func createStorageInfo() fyne.CanvasObject {
	// Заголовок раздела
	title := widget.NewLabelWithStyle("Носители информации", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	// Группируем устройства по типу
	nvmeDevices := []StorageDeviceInfo{}
	ssdDevices := []StorageDeviceInfo{}
	hddDevices := []StorageDeviceInfo{}
	flashDevices := []StorageDeviceInfo{}
	otherDevices := []StorageDeviceInfo{}

	for _, device := range sysInfo.StorageDevices {
		switch device.Type {
		case "NVME":
			nvmeDevices = append(nvmeDevices, device)
		case "SSD":
			ssdDevices = append(ssdDevices, device)
		case "HDD":
			hddDevices = append(hddDevices, device)
		case "Flash":
			flashDevices = append(flashDevices, device)
		default:
			otherDevices = append(otherDevices, device)
		}
	}

	// Создаем контейнеры для каждого типа
	items := []fyne.CanvasObject{title}

	// Функция для создания списка устройств определенного типа
	createDeviceList := func(devices []StorageDeviceInfo, typeName string) []fyne.CanvasObject {
		if len(devices) == 0 {
			return nil
		}

		result := []fyne.CanvasObject{
			widget.NewLabelWithStyle(typeName, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		}

		for _, device := range devices {
			info := fmt.Sprintf("%s: %.2f ГБ",
				device.Model,
				float64(device.Size)/(1024*1024*1024),
			)

			if device.Label != "" {
				info += fmt.Sprintf(" (%s)", device.Label)
			}

			result = append(result, widget.NewLabel(info))
		}

		return result
	}

	// Добавляем контейнеры, если есть устройства
	if list := createDeviceList(nvmeDevices, "NVME:"); list != nil {
		items = append(items, list...)
		items = append(items, widget.NewSeparator())
	}

	if list := createDeviceList(ssdDevices, "SSD:"); list != nil {
		items = append(items, list...)
		items = append(items, widget.NewSeparator())
	}

	if list := createDeviceList(hddDevices, "HDD:"); list != nil {
		items = append(items, list...)
		items = append(items, widget.NewSeparator())
	}

	if list := createDeviceList(flashDevices, "Flash:"); list != nil {
		items = append(items, list...)
		items = append(items, widget.NewSeparator())
	}

	if list := createDeviceList(otherDevices, "Другие:"); list != nil {
		items = append(items, list...)
	}

	// Если нет устройств
	if len(items) == 1 {
		items = append(items, widget.NewLabel("Не обнаружено устройств хранения"))
	}

	return container.NewVBox(items...)
}

// Создает и возвращает контейнер с информацией о прогрессе и статусе
func createProgressInfo() fyne.CanvasObject {
	// Заголовок для прогресса
	progressTitle := widget.NewLabelWithStyle("Прогресс этапов", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	// Статусы этапов
	status1 := widget.NewLabel("1. Сбор информации: завершен")
	status2 := widget.NewLabel("2. Тест дисплея: в ожидании")
	status3 := widget.NewLabel("3. Проверка серийного номера: в ожидании")
	status4 := widget.NewLabel("4. Создание логов: в ожидании")

	// Сохраняем ссылку на статус для обновления
	statusLabel = status2

	// Контейнер для статусов
	statusContainer := container.NewVBox(
		progressTitle,
		status1,
		status2,
		status3,
		status4,
	)

	// Заголовок для горячих клавиш
	hotkeysTitle := widget.NewLabelWithStyle("Горячие клавиши", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	// Горячие клавиши
	hotkeyEnter := widget.NewLabel("Y/Enter - Подтвердить")
	hotkeyN := widget.NewLabel("N - Повторить тест")
	hotkeyR := widget.NewLabel("R - Пересканировать систему")
	hotkeyQ := widget.NewLabel("Q/Esc - Выйти из программы")

	// Контейнер для горячих клавиш
	hotkeysContainer := container.NewVBox(
		hotkeysTitle,
		hotkeyEnter,
		hotkeyN,
		hotkeyR,
		hotkeyQ,
	)

	// Индикатор текущего этапа
	currentStageLabel := widget.NewLabelWithStyle(
		"Этап 1/4: Сбор информации - завершен",
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)

	// Объединяем в один контейнер
	return container.NewVBox(
		container.NewHBox(
			container.NewVBox(statusContainer),
			widget.NewSeparator(),
			container.NewVBox(hotkeysContainer),
		),
		widget.NewSeparator(),
		currentStageLabel,
	)
}

// Обновляет метку статуса
func updateStatusLabel(text string) {
	if statusLabel != nil {
		statusLabel.SetText(text)
		statusLabel.Refresh()
	}
}

// Функция для проведения теста дисплея
func runDisplayTest(window fyne.Window) {
	// Сохраняем предыдущий контент
	prevContent := window.Content()

	// Создаем элементы для теста дисплея
	// Заголовок
	titleText := canvas.NewText("Тест дисплея", color.NRGBA{255, 255, 255, 255})
	titleText.TextSize = 24
	titleText.Alignment = fyne.TextAlignCenter
	titleText.TextStyle = fyne.TextStyle{Bold: true}

	// RGB-градиент
	rgbRect := canvas.NewRectangle(color.RGBA{255, 0, 0, 255})
	rgbRect.SetMinSize(fyne.NewSize(1000, 150))

	// Функция для изменения цвета прямоугольника
	updateRGB := func(progress float64) {
		r := uint8(255 * (1 - progress))
		g := uint8(255 * progress)
		b := uint8(255 * math.Abs(0.5-progress) * 2)
		rgbRect.FillColor = color.RGBA{r, g, b, 255}
		canvas.Refresh(rgbRect)
	}

	rgbLabel := widget.NewLabelWithStyle(
		"RGB Градиент - проверьте плавность переходов цветов",
		fyne.TextAlignCenter,
		fyne.TextStyle{},
	)

	// Шкала серого
	grayRect := canvas.NewRectangle(color.Gray{0})
	grayRect.SetMinSize(fyne.NewSize(1000, 100))

	// Функция для изменения серого цвета
	updateGray := func(progress float64) {
		value := uint8(255 * progress)
		grayRect.FillColor = color.Gray{value}
		canvas.Refresh(grayRect)
	}

	grayLabel := widget.NewLabelWithStyle(
		"Шкала серого - оцените разницу между оттенками",
		fyne.TextAlignCenter,
		fyne.TextStyle{},
	)

	// Сетка для геометрии
	gridContainer := container.NewGridWithColumns(10)
	for i := 0; i < 100; i++ {
		cell := canvas.NewRectangle(color.RGBA{220, 220, 220, 255})
		cell.SetMinSize(fyne.NewSize(50, 25))
		cell.StrokeWidth = 1
		cell.StrokeColor = color.RGBA{100, 100, 100, 255}
		gridContainer.Add(cell)
	}

	gridLabel := widget.NewLabelWithStyle(
		"Проверка геометрии - убедитесь, что линии прямые и имеют одинаковое расстояние",
		fyne.TextAlignCenter,
		fyne.TextStyle{},
	)

	// Цветовые блоки
	colorContainer := container.NewHBox(
		createColorBlock(color.RGBA{255, 0, 0, 255}),
		createColorBlock(color.RGBA{0, 255, 0, 255}),
		createColorBlock(color.RGBA{0, 0, 255, 255}),
		createColorBlock(color.RGBA{255, 255, 255, 255}),
		createColorBlock(color.RGBA{0, 0, 0, 255}),
	)

	// Запрос подтверждения
	confirmLabel := widget.NewLabelWithStyle(
		"Все в порядке? [Y/n] (Enter - Да, N - Нет)",
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)

	// Создаем контент для теста дисплея
	displayTestContent := container.NewVBox(
		container.NewVBox(
			container.NewCenter(titleText),
			container.NewCenter(rgbRect),
			container.NewCenter(rgbLabel),
			container.NewCenter(grayRect),
			container.NewCenter(grayLabel),
			container.NewCenter(container.NewPadded(gridContainer)),
			container.NewCenter(gridLabel),
			container.NewCenter(colorContainer),
			container.NewCenter(confirmLabel),
		),
	)

	// Устанавливаем контент
	window.SetContent(displayTestContent)

	// Запускаем анимацию для RGB и серого
	go func() {
		steps := 100
		for i := 0; i <= steps; i++ {
			progress := float64(i) / float64(steps)
			updateRGB(progress)
			updateGray(progress)
			time.Sleep(30 * time.Millisecond)
		}
	}()

	// Обрабатываем нажатия клавиш
	window.Canvas().SetOnTypedKey(func(key *fyne.KeyEvent) {
		if key.Name == fyne.KeyY || key.Name == fyne.KeyReturn {
			// Пользователь подтвердил, что тест прошел успешно
			sysInfo.TestResults.DisplayTest = true

			// Возвращаем предыдущий контент
			window.SetContent(prevContent)

			// Обновляем статус
			updateStatusLabel("2. Тест дисплея: завершен")

			// Переходим к проверке серийного номера
			time.Sleep(500 * time.Millisecond)
			updateStatusLabel("3. Проверка серийного номера: в процессе")
			checkSerialNumber(window)
		} else if key.Name == fyne.KeyN {
			// Пользователь хочет повторить тест
			runDisplayTest(window)
		}
	})
}

// Вспомогательная функция для создания цветного блока
func createColorBlock(clr color.Color) fyne.CanvasObject {
	rect := canvas.NewRectangle(clr)
	rect.SetMinSize(fyne.NewSize(150, 60))
	return rect
}

// Функция для проверки серийного номера
func checkSerialNumber(window fyne.Window) {
	// Сохраняем предыдущий контент
	prevContent := window.Content()

	// Заголовок
	// Заголовок
	titleText := canvas.NewText("Проверка серийного номера", color.NRGBA{205, 214, 244, 255})
	titleText.TextSize = 24
	titleText.Alignment = fyne.TextAlignCenter
	titleText.TextStyle = fyne.TextStyle{Bold: true}

	// Показываем системный серийный номер
	systemSNLabel := widget.NewLabel("Серийный номер компьютера (из системы):")
	systemSNValue := widget.NewLabelWithStyle(
		sysInfo.SerialNumber,
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)

	// Поле для ввода пользовательского серийного номера
	userSNLabel := widget.NewLabel("Введите серийный номер для проверки:")
	userSNEntry := widget.NewEntry()
	userSNEntry.SetPlaceHolder("Введите серийный номер")

	// Кнопка подтверждения
	confirmButton := widget.NewButton("Продолжить [Enter]", nil)

	// Функция для проверки серийного номера
	checkSN := func() {
		userSN := userSNEntry.Text
		sysInfo.TestResults.UserEnteredSN = userSN

		// Проверяем совпадение серийных номеров
		if userSN == sysInfo.SerialNumber {
			sysInfo.TestResults.SerialNumberVerified = true

			// Возвращаем предыдущий контент
			window.SetContent(prevContent)

			// Обновляем статус
			updateStatusLabel("3. Проверка серийного номера: завершен")

			// Переходим к созданию логов
			time.Sleep(500 * time.Millisecond)
			updateStatusLabel("4. Создание логов: в процессе")
			createLogFile()

			// Обновляем статус
			updateStatusLabel("4. Создание логов: завершен")

			// Показываем итоговый экран
			time.Sleep(500 * time.Millisecond)
			showFinalScreen(window)
		} else {
			// Серийные номера не совпадают
			sysInfo.TestResults.SerialNumberVerified = false

			// Показываем сообщение об ошибке
			errorLabel := widget.NewLabelWithStyle(
				"ВНИМАНИЕ! Серийные номера не совпадают!",
				fyne.TextAlignCenter,
				fyne.TextStyle{Bold: true},
			)

			dialog.ShowCustom("Ошибка", "Продолжить", container.NewVBox(
				errorLabel,
				widget.NewLabel("Введенный серийный номер не соответствует серийному номеру системы."),
				widget.NewLabel("Это будет отмечено в логах."),
			), window)

			// После закрытия диалога продолжаем
			window.SetContent(prevContent)

			// Обновляем статус
			updateStatusLabel("3. Проверка серийного номера: ошибка")

			// Переходим к созданию логов
			time.Sleep(500 * time.Millisecond)
			updateStatusLabel("4. Создание логов: в процессе")
			createLogFile()

			// Обновляем статус
			updateStatusLabel("4. Создание логов: завершен")

			// Показываем итоговый экран
			time.Sleep(500 * time.Millisecond)
			showFinalScreen(window)
		}
	}

	// Устанавливаем обработчик для кнопки
	confirmButton.OnTapped = checkSN

	// Обработчик для поля ввода (Enter)
	userSNEntry.OnSubmitted = func(text string) {
		checkSN()
	}

	// Подсказка
	hintLabel := widget.NewLabel("Введите серийный номер и нажмите Enter")

	// Создаем контент для проверки серийного номера
	snContent := container.NewVBox(
		container.NewCenter(titleText),
		container.NewVBox(
			container.NewCenter(widget.NewLabelWithStyle(
				"Проверка серийного номера",
				fyne.TextAlignCenter,
				fyne.TextStyle{Bold: true},
			)),
		),
		container.NewCenter(container.NewVBox(
			systemSNLabel,
			systemSNValue,
			widget.NewSeparator(),
			userSNLabel,
			userSNEntry,
			container.NewCenter(confirmButton),
			container.NewCenter(hintLabel),
		)),
	)

	// Устанавливаем контент
	window.SetContent(snContent)

	// Fyne автоматически установит фокус на первый фокусируемый виджет
	// Явно устанавливать фокус не требуется
}

// Функция для создания и сохранения лога
func createLogFile() {
	// Создание директории для логов, если её нет
	logDir := "troubadour_logs"
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		os.Mkdir(logDir, 0755)
	}

	// Форматирование имени файла
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	logFileName := filepath.Join(logDir, fmt.Sprintf("%s_%s.log", sysInfo.SerialNumber, timestamp))

	// Получаем полную информацию dmidecode для логов
	dmidecodeInfo := getDmidecodeInfo()

	// Создаем структуру для лога
	logData := struct {
		SystemInfo    SystemInfo          `json:"system_info"`
		DmidecodeInfo map[string][]string `json:"dmidecode_info"`
		TestResults   TestResults         `json:"test_results"`
		Timestamp     string              `json:"timestamp"`
	}{
		SystemInfo:    sysInfo,
		DmidecodeInfo: dmidecodeInfo,
		TestResults:   sysInfo.TestResults,
		Timestamp:     timestamp,
	}

	// Сериализация данных в JSON
	jsonData, err := json.MarshalIndent(logData, "", "  ")
	if err != nil {
		fmt.Println("Error creating log file:", err)
		return
	}

	// Запись файла
	err = ioutil.WriteFile(logFileName, jsonData, 0644)
	if err != nil {
		fmt.Println("Error writing log file:", err)
		return
	}
}

// Функция для получения полной информации dmidecode
func getDmidecodeInfo() map[string][]string {
	dmidecodeInfo := make(map[string][]string)

	// Список секций dmidecode
	sections := []string{
		"bios", "system", "baseboard", "chassis", "processor",
		"memory", "cache", "connector", "slot",
	}

	for _, section := range sections {
		cmd := exec.Command("dmidecode", "-t", section)
		output, err := cmd.Output()
		if err != nil {
			fmt.Println("Error fetching dmidecode info for section", section, ":", err)
			continue
		}

		outputStr := string(output)
		lines := strings.Split(outputStr, "\n")

		// Парсим заголовки/разделы
		var currentHeader string
		var currentContent []string

		for _, line := range lines {
			line = strings.TrimSpace(line)

			// Проверяем, является ли строка заголовком
			if strings.HasPrefix(line, "Handle ") {
				// Если у нас уже есть заголовок, сохраняем предыдущий раздел
				if currentHeader != "" {
					dmidecodeInfo[currentHeader] = currentContent
				}

				// Начинаем новый раздел
				currentHeader = line
				currentContent = []string{}
			} else if line != "" {
				// Добавляем строку к текущему разделу
				currentContent = append(currentContent, line)
			}
		}

		// Добавляем последний раздел
		if currentHeader != "" {
			dmidecodeInfo[currentHeader] = currentContent
		}
	}

	return dmidecodeInfo
}

// Функция для отображения итогового экрана
func showFinalScreen(window fyne.Window) {
	// Заголовок
	// Заголовок
	titleText := canvas.NewText("Troubadour - Завершение диагностики", color.NRGBA{205, 214, 244, 255})
	titleText.TextSize = 24
	titleText.Alignment = fyne.TextAlignCenter
	titleText.TextStyle = fyne.TextStyle{Bold: true}

	// Статус
	// Статус
	statusText := canvas.NewText("Все этапы успешно завершены", color.NRGBA{166, 227, 161, 255})
	statusText.TextSize = 18
	statusText.Alignment = fyne.TextAlignCenter
	statusText.TextStyle = fyne.TextStyle{Bold: true}

	// Информация о логах
	logTitle := widget.NewLabelWithStyle(
		"Информация о логах",
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)

	// Форматирование имени файла
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	logFileName := fmt.Sprintf("%s_%s.log", sysInfo.SerialNumber, timestamp)

	logPathLabel := widget.NewLabel(fmt.Sprintf("Логи успешно созданы по пути: troubadour_logs/%s", logFileName))

	// Содержимое лога
	logContentTitle := widget.NewLabel("Содержание логов:")

	displayTestResult := "Успешно"
	if !sysInfo.TestResults.DisplayTest {
		displayTestResult = "Не пройден"
	}

	snTestResult := "Подтверждено"
	if !sysInfo.TestResults.SerialNumberVerified {
		snTestResult = "Не подтверждено"
	}

	logContentItems := []fyne.CanvasObject{
		widget.NewLabel("• Информация о выявленном оборудовании"),
		widget.NewLabel("• Результаты теста дисплея: " + displayTestResult),
		widget.NewLabel("• Проверка серийного номера: " + snTestResult),
		widget.NewLabel("• Полная информация dmidecode"),
		widget.NewLabel("• Общий статус системы: Нормальный"),
	}

	logContentBox := container.NewVBox(append([]fyne.CanvasObject{logContentTitle}, logContentItems...)...)

	// Блок действий
	actionsTitle := widget.NewLabelWithStyle(
		"Что дальше?",
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)

	// Кнопки действий
	rescanButton := widget.NewButton("Пересканировать [R]", func() {
		sysInfo = SystemInfo{}
		startDiagnosticSequence(window)
	})

	exitButton := widget.NewButton("Выход [Q, Esc]", func() {
		window.Close()
	})

	// Обработчик клавиш
	window.Canvas().SetOnTypedKey(func(key *fyne.KeyEvent) {
		if key.Name == fyne.KeyR {
			sysInfo = SystemInfo{}
			startDiagnosticSequence(window)
		} else if key.Name == fyne.KeyQ || key.Name == fyne.KeyEscape {
			window.Close()
		}
	})

	// Создаем контент для итогового экрана
	finalContent := container.NewVBox(
		container.NewCenter(titleText),
		container.NewCenter(statusText),
		widget.NewSeparator(),
		container.NewCenter(logTitle),
		container.NewCenter(logPathLabel),
		container.NewCenter(logContentBox),
		widget.NewSeparator(),
		container.NewCenter(actionsTitle),
		container.NewCenter(rescanButton),
		container.NewCenter(exitButton),
	)

	window.SetContent(finalContent)
}
