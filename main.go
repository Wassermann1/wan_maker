package main

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

func main() {
	// Шаг 0: Определение имени проводного интерфейса
	interfaces := getWiredInterfaces()

	if len(interfaces) == 0 {
		fmt.Println("Не найдено активных проводных интерфейсов.")
		fmt.Println("Нажмите любую клавишу для завершения...")
		fmt.Scanln()
		return
	}

	var selectedInterface string
	if len(interfaces) == 1 {
		selectedInterface = interfaces[0]
		fmt.Println("Используется интерфейс:", selectedInterface)
	} else {
		selectedInterface = userSelect(interfaces)
		fmt.Println("Выбран интерфейс:", selectedInterface)
	}

	fmt.Printf("Определен проводной интерфейс: %s\n", selectedInterface)
	if runtime.GOOS == "windows" {
		if !isOpenSSHInstalled() {
			fmt.Println("OpenSSH не установлен. Пожалуйста, установите OpenSSH и повторите попытку.")
			fmt.Println("Нажмите любую клавишу для завершения...")
			fmt.Scanln()
			return
		}
	}

	// Шаг 1: Установка статического IP-адреса
	setStaticIP(selectedInterface, "192.168.1.100", "255.255.255.0")

	fmt.Println("\nИнструкция:")
	fmt.Println("1 - Заходим в веб-морду 192.168.1.254, логин: mgts, пароль: mtsoao")
	fmt.Println("2 - Настраиваем Wi-Fi")
	fmt.Println("3 - Отключаем DHCP")
	fmt.Println("4 - Включаем русский язык")
	fmt.Println("\nДля продолжения нажмите ENTER, и будет открыт браузер...")
	fmt.Scanln()

	// Шаг 3: Открытие браузера
	openBrowser("http://192.168.1.254")
	fmt.Println("Обязательно примените настройки на роутере, иначе дальнейшее исполнение программы превратит его в кирпич!")

	// Шаг 4: Запрос подтверждения о применении настроек
	if !confirmSettingsApplied() {
		setDynamicIP(selectedInterface)
		fmt.Println("Изменения отменены.")
		fmt.Println("Нажмите любую клавишу для завершения...")
		fmt.Scanln()
		return
	}

	// Шаг 2: Подключение по SSH
	client, err := connectSSH("192.168.1.254", "mgts", "mtsoao")
	if err != nil {
		fmt.Println("Невозможно подключиться по SSH: ", err)
		setDynamicIP(selectedInterface)
		fmt.Println("Изменения отменены.")
		fmt.Println("Нажмите любую клавишу для завершения...")
		fmt.Scanln()
		return
	}
	defer client.Close()

	// Шаг 3: Запрос диапазона LAN у пользователя
	fmt.Print("Введите диапазон LAN в формате X.X.X.1: ")
	reader := bufio.NewReader(os.Stdin)
	lanAddress, _ := reader.ReadString('\n')
	lanAddress = strings.TrimSpace(lanAddress)

	// Проверка ввода
	if !strings.HasSuffix(lanAddress, ".1") {
		fmt.Println("Неверный формат LAN-адреса. Должен заканчиваться на .1")
		fmt.Println("Изменения отменены.")
		fmt.Println("Нажмите любую клавишу для завершения...")
		fmt.Scanln()
		return
	}

	// Генерация диапазонов DHCP
	lanDHCPStart := strings.TrimSuffix(lanAddress, ".1") + ".100"
	lanDHCPEnd := strings.TrimSuffix(lanAddress, ".1") + ".199"

	// Шаг 4: Чтение и изменение скрипта
	script := readAndModifyScript(lanAddress, lanDHCPStart, lanDHCPEnd)

	// Шаг 5: Передача скрипта на устройство через SSH
	executeScriptOverSSH(client, script)

	// Шаг 6: Перезагрузка устройства
	rebootDevice(client)

	// Шаг 7: Ожидание доступности нового IP-адреса
	waitForLAN(lanAddress)

	// Шаг 8: Смена конфигурации на динамическую
	setDynamicIP(selectedInterface)

	// Сообщение об успехе
	fmt.Println("Настройка завершена успешно!")
	fmt.Println("Нажмите любую клавишу для завершения...")
	fmt.Scanln()
}

// getWiredInterfaces ищет активные проводные интерфейсы
func getWiredInterfaces() []string {
	switch runtime.GOOS {
	case "windows":
		return getWiredInterfacesWindows()
	case "linux":
		return getWiredInterfacesLinux()
	default:
		fmt.Printf("Операционная система %s не поддерживается\n", runtime.GOOS)
		fmt.Println("Изменения отменены.")
		fmt.Println("Нажмите любую клавишу для завершения...")
		fmt.Scanln()
		return nil
	}
}

// getWiredInterfacesWindows ищет активные проводные интерфейсы в Windows
func getWiredInterfacesWindows() []string {
	cmd := exec.Command("netsh", "interface", "show", "interface")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Ошибка при получении списка интерфейсов: %v", err)
	}

	var interfaces []string
	lines := strings.Split(out.String(), "\n")
	fmt.Println(lines)
	for _, line := range lines {
		// Ищем "Connected" и "Ethernet"
		if (strings.Contains(line, "Connected") || strings.Contains(line, "Подключен")) && strings.Contains(line, "Ethernet") {
			fields := strings.Fields(line)
			// Имя интерфейса всегда в **последнем столбце**
			if len(fields) > 3 {
				interfaces = append(interfaces, fields[len(fields)-1])
			}
		}
	}
	return interfaces
}

// getWiredInterfacesLinux ищет активные проводные интерфейсы в Linux
func getWiredInterfacesLinux() []string {
	cmd := exec.Command("ip", "link")
	output, err := cmd.Output()
	if err != nil {
		log.Fatalf("Ошибка при получении списка интерфейсов: %v", err)
	}

	var interfaces []string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		// Ищем строки, содержащие имена интерфейсов (кроме lo)
		if strings.Contains(line, ": <") && !strings.Contains(line, "lo") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				interfaceName := strings.TrimRight(parts[1], ":")
				// Проверяем, что это проводной интерфейс (обычно "eth*" или "en*")
				if strings.HasPrefix(interfaceName, "e") {
					// Проверяем состояние интерфейса
					if strings.Contains(line, "state UP") || strings.Contains(line, "NO-CARRIER") {
						interfaces = append(interfaces, interfaceName)
					}
				}
			}
		}
	}
	return interfaces
}

// userSelect предлагает пользователю выбрать интерфейс, если их несколько
func userSelect(options []string) string {
	fmt.Println("Доступные проводные интерфейсы:")
	for i, iface := range options {
		fmt.Printf("%d. %s\n", i+1, iface)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Выберите номер интерфейса: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		var choice int
		_, err := fmt.Sscanf(input, "%d", &choice)
		if err == nil && choice > 0 && choice <= len(options) {
			return options[choice-1]
		}

		fmt.Println("Ошибка: введите корректный номер из списка.")
	}
}

// Проверка наличия OpenSSH
func isOpenSSHInstalled() bool {
	cmd := exec.Command("sc", "query", "OpenSSH")
	err := cmd.Run()
	return err == nil
}

func setStaticIP(interfaceName, ip, netmask string) {
	switch runtime.GOOS {
	case "linux":
		// Поднимаем интерфейс, если он в состоянии DOWN
		cmdUp := exec.Command("sudo", "ip", "link", "set", interfaceName, "up")
		err := cmdUp.Run()
		if err != nil {
			log.Fatalf("Ошибка при поднятии интерфейса %s: %v", interfaceName, err)
		}
		fmt.Printf("Интерфейс %s поднят\n", interfaceName)

		// Настраиваем статический IP
		cmd := exec.Command("sudo", "ifconfig", interfaceName, ip, "netmask", netmask, "up")
		err = cmd.Run()
		if err != nil {
			log.Fatalf("Ошибка при установке статического IP: %v", err)
		}
		fmt.Printf("Установлен статический IP: %s/%s\n", ip, netmask)

	case "windows":
		// Для Windows команда автоматически поднимает интерфейс
		err := exec.Command("netsh", "interface", "ip", "set", "address", fmt.Sprintf("name=%q", interfaceName), "static", ip, netmask, "1").Run()
		if err != nil {
			log.Fatalf("Ошибка при установке статического IP: %v", err)
		}
		fmt.Printf("Установлен статический IP: %s/%s\n", ip, netmask)

	default:
		log.Fatalf("Операционная система %s не поддерживается", runtime.GOOS)
	}
}

// Подключение по SSH
func connectSSH(host, user, password string) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", host+":22", config)
	if err != nil {
		return nil, err
	}
	fmt.Println("Успешное подключение по SSH")
	return client, nil
}

// Чтение и изменение скрипта
func readAndModifyScript(lanAddress, lanDHCPStart, lanDHCPEnd string) string {
	return fmt.Sprintf(script, lanAddress, lanDHCPStart, lanDHCPEnd)
}

// Выполнение скрипта через SSH
func executeScriptOverSSH(client *ssh.Client, script string) {
	session, err := client.NewSession()
	if err != nil {
		log.Fatalf("Ошибка создания сессии SSH: %v", err)
	}
	defer session.Close()

	// Отправка скрипта как ввод
	session.Stdin = strings.NewReader(script)
	err = session.Run("sh")
	if err != nil {
		log.Fatalf("Ошибка выполнения скрипта: %v", err)
	}
	fmt.Println("Скрипт успешно выполнен на удаленном устройстве")
}

// Перезагрузка устройства
func rebootDevice(client *ssh.Client) {
	session, err := client.NewSession()
	if err != nil {
		log.Fatalf("Ошибка создания сессии SSH: %v", err)
	}
	defer session.Close()

	err = session.Run("reboot")
	if err != nil {
		log.Fatalf("Ошибка перезагрузки устройства: %v", err)
	}
	fmt.Println("Устройство перезагружено")
}

// Ожидание доступности нового IP-адреса
func waitForLAN(ip string) {
	fmt.Printf("Ожидание доступности IP-адреса %s...\n", ip)
	for {
		conn, err := net.DialTimeout("tcp", ip+":22", 2*time.Second)
		if err == nil {
			conn.Close()
			fmt.Println("IP-адрес доступен!")
			break
		}
		time.Sleep(2 * time.Second)
	}
}

// Смена конфигурации на динамическую
func setDynamicIP(interfaceName string) {
	switch runtime.GOOS {
	case "linux":
		cmd := exec.Command("sudo", "ip", "addr", "flush", "dev", interfaceName)
		err := cmd.Run()
		if err != nil {
			log.Fatalf("Ошибка при смене конфигурации на динамическую: %v", err)
		}
		fmt.Println("Конфигурация изменена на динамическую")
	case "windows":
		cmd := fmt.Sprintf("netsh interface ip set address name=\"%s\" dhcp", interfaceName)
		err := exec.Command("cmd", "/C", cmd).Run()
		if err != nil {
			log.Fatalf("Ошибка при смене конфигурации на динамическую: %v", err)
		}
		fmt.Println("Конфигурация изменена на динамическую")
	default:
		log.Fatalf("Операционная система %s не поддерживается", runtime.GOOS)
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		log.Fatalf("Операционная система %s не поддерживается", runtime.GOOS)
	}
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Ошибка при открытии браузера: %v", err)
	}
	fmt.Printf("Открыт браузер на странице: %s\n", url)
}

// Запрос подтверждения о применении настроек
func confirmSettingsApplied() bool {
	for {
		fmt.Print("Настройки применены? (y/n): ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))

		switch input {
		case "y", "yes", "да", "д":
			return true
		case "n", "no", "нет":
			return false
		default:
			fmt.Println("Неверный ввод. Введите 'y' или 'n'.")
		}
	}
}
