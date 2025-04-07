package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"golang.org/x/sync/errgroup"
)

// Конфигурация
const (
	CONST_TOKEN = "enter_token" // Замените на ваш токен бота
	CONST_PATH  = "./data"         // Путь для сохранения файлов
)

// Словарь для соответствия страны и валюты
var countryCurrencyDict = map[string]string{
	"DZ": "DZD", "AU": "AUD", "BH": "BHD", "BD": "BDT", "BO": "BOB", "BR": "BRL",
	"KH": "KHR", "CA": "CAD", "KYD": "KYD", "CL": "CLP", "CO": "COP", "CR": "CRC",
	"EG": "EGP", "GE": "GEL", "GH": "GHS", "HK": "HKD", "IN": "INR", "ID": "IDR",
	"IQ": "IQD", "IL": "ILS", "JP": "JPY", "JO": "JOD", "KZ": "KZT", "KE": "KES",
	"KR": "KRW", "KW": "KWD", "MO": "MOP", "MY": "MYR", "MX": "MXN", "MA": "MAD",
	"MM": "MMK", "NZ": "NZD", "NG": "NGN", "OM": "OMR", "PK": "PKR", "PA": "PAB",
	"PY": "PYG", "PE": "PEN", "PH": "PHP", "QA": "QAR", "RU": "RUB", "SA": "SAR",
	"RS": "RSD", "SG": "SGD", "ZA": "ZAR", "LK": "LKR", "TW": "TWD", "TZ": "TZS",
	"TH": "THB", "TR": "TRY", "UA": "UAH", "AE": "AED", "US": "USD", "VN": "VND",
}

// Список стран для парсинга
var countries = []string{
	"DZ", "EG", "AU", "BD", "BO", "BR", "CA", "CL", "CO", "CR", "GE", "GH", "HK", "IN", "ID", "IQ",
	"IL", "JP", "JO", "KZ", "KE", "KR", "MO", "MY", "MX", "MA", "MM", "NZ", "NG", "PK", "PY", "PE",
	"PH", "QA", "RU", "SA", "RS", "SG", "ZA", "LK", "TW", "TZ", "TH", "TR", "UA", "AE", "US", "VN",
}

// Курсы валют к USD
var currencyRates = map[string]float64{
	"DZD": 134.966, "AUD": 1.583982, "BHD": 0.376241, "BDT": 109.73,
	"BOB": 6.909550, "BRL": 5.806974, "CAD": 1.433827, "KYD": 0.833,
	"CLP": 961.794638, "COP": 4153.599492, "CRC": 504.817577, "EGP": 50.291311,
	"GEL": 2.867107, "GHS": 15.187930, "HKD": 7.787505, "INR": 86.249922,
	"IDR": 16149.393463, "IQD": 1309.703222, "ILS": 3.587793, "JPY": 155.855438,
	"JOD": 0.709118, "KZT": 519.503277, "KES": 129.264801, "KRW": 1432.185253,
	"KWD": 0.308060, "MOP": 8.021963, "MYR": 4.392292, "MXN": 20.245294,
	"MAD": 10.007902, "MMK": 2099.980901, "NZD": 1.752597, "NGN": 1550.620034,
	"OMR": 0.384454, "PKR": 278.655722, "PYG": 7918.619687, "PEN": 3.712514,
	"PHP": 58.388686, "QAR": 3.639992, "RUB": 97.929483, "SAR": 3.750482,
	"RSD": 112.125584, "SGD": 1.348339, "ZAR": 18.384263, "LKR": 298.761937,
	"TWD": 32.687009, "TZS": 2507.601986, "THB": 33.712166, "TRY": 35.678472,
	"UAH": 41.939132, "AED": 3.671703, "USD": 1, "VND": 25094.287781,
}

// Структура для хранения найденных цен
type PriceData struct {
	MinPriceUSD   float64
	MaxPriceUSD   float64
	CountryCode   string
	CurrencyCode  string
	OriginalPrice string
}

// Функция для очистки строки цены
func cleanPrice(price string) string {
	price = strings.ReplaceAll(price, " per item", "")
	price = strings.ReplaceAll(price, "\u00a0", " ") // Замена непечатаемого пробела
	return strings.TrimSpace(price)
}

// Функция для округления числа до заданного количества знаков после запятой
func round(num float64, precision int) float64 {
	mult := math.Pow10(precision)
	return math.Round(num*mult) / mult
}

// Функция для парсинга цены в зависимости от валюты
func parsePrice(priceStr string, currencyCode string) (float64, error) {
	priceStr = strings.TrimSpace(priceStr)
	
	// Сначала удаляем символы валют и прочие нечисловые символы
	switch currencyCode {
	case "IDR":
		priceStr = strings.ReplaceAll(priceStr, "Rp ", "")
	case "JOD":
		priceStr = strings.ReplaceAll(priceStr, "JOD ", "")
	case "TRY":
		priceStr = strings.ReplaceAll(priceStr, "TRY ", "")
	case "JPY":
		priceStr = strings.ReplaceAll(priceStr, "¥", "")
	case "KRW":
		priceStr = strings.ReplaceAll(priceStr, "₩", "")
	case "INR":
		priceStr = strings.ReplaceAll(priceStr, "₹", "")
	case "VND":
		priceStr = strings.ReplaceAll(priceStr, "₫", "")
	case "HKD":
		priceStr = strings.ReplaceAll(priceStr, "HK$", "")
	case "TWD":
		priceStr = strings.ReplaceAll(priceStr, "NT$", "")
	case "USD", "AUD", "NZD", "CAD", "SGD":
		priceStr = strings.ReplaceAll(priceStr, "$", "")
	case "ILS":
		priceStr = strings.ReplaceAll(priceStr, "₪", "")
	case "ZAR":
		priceStr = strings.ReplaceAll(priceStr, "R ", "")
		priceStr = strings.ReplaceAll(priceStr, " ", "")
	}
	
	// Удаляем специальные суффиксы
	priceStr = strings.ReplaceAll(priceStr, " per item", "")
	
	// Ищем числовое значение в строке
	re := regexp.MustCompile(`[\d,.]+`)
	matches := re.FindString(priceStr)
	if matches == "" {
		return 0, fmt.Errorf("Не удалось найти числовое значение в строке: %s", priceStr)
	}
	
	// Универсальная обработка форматов цен для всех стран
	// Определяем, какой формат используется
	hasComma := strings.Contains(matches, ",")
	hasDot := strings.Contains(matches, ".")
	
	var numStr string
	
	// Определяем формат цены на основе анализа строки
	if hasComma && hasDot {
		// Случай, когда в строке есть и точки, и запятые (например, "1.234,56")
		// Обычно точка - разделитель тысяч, запятая - десятичная
		numStr = strings.ReplaceAll(matches, ".", "")
		numStr = strings.ReplaceAll(numStr, ",", ".")
	} else if hasDot {
		// Проверяем, является ли точка разделителем тысяч или десятичной точкой
		parts := strings.Split(matches, ".")
		if len(parts) > 2 {
			// Если точек больше одной, то это разделители тысяч, например "1.234.567"
			numStr = strings.ReplaceAll(matches, ".", "")
		} else if len(parts[len(parts)-1]) <= 2 {
			// Если после последней точки 1-2 цифры, это, вероятно, десятичная точка
			numStr = matches
		} else {
			// Иначе считаем, что это разделитель тысяч
			numStr = strings.ReplaceAll(matches, ".", "")
		}
	} else if hasComma {
		// Проверяем, является ли запятая разделителем тысяч или десятичной запятой
		parts := strings.Split(matches, ",")
		if len(parts) > 2 {
			// Если запятых больше одной, то это разделители тысяч, например "1,234,567"
			numStr = strings.ReplaceAll(matches, ",", "")
		} else if len(parts[len(parts)-1]) <= 2 {
			// Если после последней запятой 1-2 цифры, это, вероятно, десятичная запятая
			numStr = strings.ReplaceAll(matches, ",", ".")
		} else {
			// Иначе считаем, что это разделитель тысяч
			numStr = strings.ReplaceAll(matches, ",", "")
		}
	} else {
		// Если нет ни точек, ни запятых, используем строку как есть
		numStr = matches
	}
	
	// Пытаемся преобразовать строку в float
	return strconv.ParseFloat(numStr, 64)
}

// Функция для конвертации строки цены в USD
func convertPriceToUSD(priceStr string, currencyCode string) (float64, float64, error) {
	// Очищаем строку от лишних пробелов
	priceStr = strings.TrimSpace(priceStr)
	
	// Проверяем, есть ли в строке диапазон цен "X - Y" или "X; Y"
	var firstRange string
	if strings.Contains(priceStr, ";") {
		parts := strings.Split(priceStr, ";")
		firstRange = strings.TrimSpace(parts[0])
	} else {
		firstRange = priceStr
	}
	
	var minPrice, maxPrice float64
	var err error
	
	if strings.Contains(firstRange, "-") {
		// Диапазон цен
		rangeParts := strings.Split(firstRange, "-")
		if len(rangeParts) < 2 {
			return 0, 0, fmt.Errorf("Некорректный формат диапазона цен: %s", firstRange)
		}
		
		minPriceStr := strings.TrimSpace(rangeParts[0])
		maxPriceStr := strings.TrimSpace(rangeParts[1])
		
		// Проверка на пустые строки
		if minPriceStr == "" || maxPriceStr == "" {
			return 0, 0, fmt.Errorf("Пустая строка в диапазоне цен: %s", firstRange)
		}
		
		// Парсим минимальную цену
		minPrice, err = parsePrice(minPriceStr, currencyCode)
		if err != nil {
			// Если произошла ошибка, логируем её и пробуем просто извлечь первое число
			log.Printf("Ошибка при парсинге минимальной цены '%s': %v, пытаемся извлечь число", minPriceStr, err)
			re := regexp.MustCompile(`\d+`)
			numStr := re.FindString(minPriceStr)
			if numStr != "" {
				minPrice, err = strconv.ParseFloat(numStr, 64)
				if err != nil {
					return 0, 0, fmt.Errorf("Ошибка при парсинге минимальной цены: %v", err)
				}
			} else {
				return 0, 0, fmt.Errorf("Не удалось извлечь число из минимальной цены: %s", minPriceStr)
			}
		}
		
		// Парсим максимальную цену
		maxPrice, err = parsePrice(maxPriceStr, currencyCode)
		if err != nil {
			// Если произошла ошибка, логируем её и пробуем просто извлечь первое число
			log.Printf("Ошибка при парсинге максимальной цены '%s': %v, пытаемся извлечь число", maxPriceStr, err)
			re := regexp.MustCompile(`\d+`)
			numStr := re.FindString(maxPriceStr)
			if numStr != "" {
				maxPrice, err = strconv.ParseFloat(numStr, 64)
				if err != nil {
					return 0, 0, fmt.Errorf("Ошибка при парсинге максимальной цены: %v", err)
				}
			} else {
				return 0, 0, fmt.Errorf("Не удалось извлечь число из максимальной цены: %s", maxPriceStr)
			}
		}
	} else {
		// Одиночная цена
		minPrice, err = parsePrice(firstRange, currencyCode)
		if err != nil {
			// Если произошла ошибка, логируем её и пробуем просто извлечь первое число
			log.Printf("Ошибка при парсинге цены '%s': %v, пытаемся извлечь число", firstRange, err)
			re := regexp.MustCompile(`\d+`)
			numStr := re.FindString(firstRange)
			if numStr != "" {
				minPrice, err = strconv.ParseFloat(numStr, 64)
				if err != nil {
					return 0, 0, fmt.Errorf("Ошибка при парсинге цены: %v", err)
				}
			} else {
				return 0, 0, fmt.Errorf("Не удалось извлечь число из цены: %s", firstRange)
			}
		}
		maxPrice = minPrice
	}
	
	// Получаем курс валюты или используем 1, если не найден
	rate, exists := currencyRates[currencyCode]
	if !exists || rate == 0 {
		rate = 1.0
	}
	
	// Конвертируем в USD и округляем
	minUSD := math.Max(round(minPrice/rate, 2), 0.01)
	maxUSD := math.Max(round(maxPrice/rate, 2), 0.01)
	
	return minUSD, maxUSD, nil
}

// Функция для получения цен для конкретной страны
func getPricesForCountry(countryCode string, appID string) ([]string, string, error) {
	currencyCode, exists := countryCurrencyDict[countryCode]
	if !exists {
		currencyCode = "USD"
	}
	
	url := fmt.Sprintf("https://play.google.com/store/apps/details?id=%s&hl=en&gl=%s", appID, countryCode)
	
	// Создаем транспорт с возможностью повторных попыток
	client := &http.Client{
		Timeout: 15 * time.Second, // Увеличиваем таймаут для медленных соединений
	}
	
	// Делаем запрос с повторными попытками в случае неудачи
	var resp *http.Response
	var err error
	maxRetries := 2
	
	for i := 0; i <= maxRetries; i++ {
		resp, err = client.Get(url)
		if err == nil {
			break
		}
		
		if i < maxRetries {
			log.Printf("[Google] %s: Повторная попытка запроса (%d из %d) после ошибки: %v", 
				countryCode, i+1, maxRetries, err)
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}
	
	if err != nil {
		return nil, currencyCode, fmt.Errorf("Ошибка запроса после %d попыток: %v", maxRetries+1, err)
	}
	defer resp.Body.Close()
	
	// Проверяем код ответа
	if resp.StatusCode != http.StatusOK {
		return nil, currencyCode, fmt.Errorf("Ошибка HTTP: %d", resp.StatusCode)
	}
	
	// Читаем содержимое страницы
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, currencyCode, fmt.Errorf("Ошибка чтения ответа: %v", err)
	}
	
	pageContent := string(bodyBytes)
	
	fmt.Printf("[Google] %s\n", countryCode)
	
	// Проверяем наличие ошибки 404
	if strings.Contains(pageContent, "We're sorry, the requested URL was not found on this server.") {
		return nil, currencyCode, fmt.Errorf("404")
	}
	
	// Проверяем наличие покупок в приложении
	if !strings.Contains(pageContent, "In-app purchases") {
		return nil, currencyCode, fmt.Errorf("На странице нет текста 'In-app purchases'")
	}
	
	// Ищем шаблон, например, "XXX per item"
	// Используем несколько регулярных выражений для поиска цен в разных форматах
	patterns := []string{
		`"([^"]*?\sper\sitem)"`,
		`"([^"]*?)"[^>]*?>\s*per item`,
		`>([^<]*?\sper\sitem)<`,
	}
	
	var prices []string
	
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(pageContent, -1)
		
		for _, match := range matches {
			if len(match) > 1 && match[1] != "" {
				price := strings.TrimSpace(match[1])
				if price != "" {
					prices = append(prices, price)
				}
			}
		}
		
		// Если нашли хотя бы одну цену, прекращаем поиск
		if len(prices) > 0 {
			break
		}
	}
	
	// Если цены не найдены, пробуем другие варианты
	if len(prices) == 0 {
		// Попробуем найти просто цены без "per item"
		rePrice := regexp.MustCompile(`"(\$[\d,.]+ - \$[\d,.]+)"`)
		matches := rePrice.FindAllStringSubmatch(pageContent, -1)
		
		for _, match := range matches {
			if len(match) > 1 && match[1] != "" {
				prices = append(prices, match[1]+" per item")
			}
		}
	}
	
	if len(prices) == 0 {
		return nil, currencyCode, fmt.Errorf("Не найдены цены")
	}
	
	return prices, currencyCode, nil
}

// Функция для получения цен для всех стран
func fetchPricesGoogle(appID string, chatID int64, bot *tgbotapi.BotAPI) (string, error) {
	msg := tgbotapi.NewMessage(chatID, "Обработка для Google Play началась...")
	_, err := bot.Send(msg)
	if err != nil {
		return "", fmt.Errorf("Ошибка отправки сообщения: %v", err)
	}
	
	var priceData []PriceData
	var mu sync.Mutex // Для безопасной записи в общий слайс
	// var errorCount int32 = 0 // Счетчик ошибок
	
	// Используем errgroup для параллельной обработки
	g, ctx := errgroup.WithContext(context.Background())
	sem := make(chan struct{}, 5) // Ограничиваем количество параллельных запросов
	
	for _, countryCode := range countries {
		countryCode := countryCode // Создаем локальную копию для горутины
		
		// Проверяем, не был ли контекст отменен
		select {
		case <-ctx.Done():
			break
		default:
			sem <- struct{}{} // Занимаем слот
			g.Go(func() error {
				defer func() { <-sem }() // Освобождаем слот
				
				// Устанавливаем обработчик паники
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[Google] %s: Восстановление после паники: %v", countryCode, r)
					}
				}()
				
				prices, currencyCode, err := getPricesForCountry(countryCode, appID)
				if err != nil {
					log.Printf("[Google] %s: %v", countryCode, err)
					return nil // Не прерываем выполнение всей группы из-за ошибки одной страны
				}
				
				if len(prices) > 0 {
					minPriceUSD, maxPriceUSD, err := convertPriceToUSD(prices[0], currencyCode)
					if err != nil {
						log.Printf("Ошибка при конвертации цены для %s: %v", countryCode, err)
						return nil
					}
					
					mu.Lock()
					priceData = append(priceData, PriceData{
						MinPriceUSD:   minPriceUSD,
						MaxPriceUSD:   maxPriceUSD,
						CountryCode:   countryCode,
						CurrencyCode:  currencyCode,
						OriginalPrice: prices[0],
					})
					mu.Unlock()
				}
				
				return nil
			})
		}
	}
	
	// Ждем завершения всех горутин
	if err := g.Wait(); err != nil {
		log.Printf("Предупреждение: возникла ошибка в одной из горутин: %v", err)
		// Продолжаем выполнение с теми данными, которые удалось получить
	}
	
	// Проверяем, есть ли данные
	if len(priceData) == 0 {
		return "", fmt.Errorf("Не удалось получить данные о ценах ни для одной страны")
	}
	
	// Отправляем сообщение о статусе обработки
	statusMsg := fmt.Sprintf("Обработка завершена. Получена информация о ценах для %d из %d стран.", 
		len(priceData), len(countries))
	bot.Send(tgbotapi.NewMessage(chatID, statusMsg))
	
	// Сортируем данные по минимальной цене
	sort.Slice(priceData, func(i, j int) bool {
		return priceData[i].MinPriceUSD < priceData[j].MinPriceUSD
	})
	
	// Создаем директорию, если не существует
	if _, err := os.Stat(CONST_PATH); os.IsNotExist(err) {
		err := os.MkdirAll(CONST_PATH, 0755)
		if err != nil {
			return "", fmt.Errorf("Ошибка создания директории: %v", err)
		}
	}
	
	// Создаем CSV-файл
	filepath := filepath.Join(CONST_PATH, fmt.Sprintf("%s_google.csv", appID))
	file, err := os.Create(filepath)
	if err != nil {
		return "", fmt.Errorf("Ошибка создания файла: %v", err)
	}
	defer file.Close()
	
	// Пишем данные в CSV
	writer := csv.NewWriter(file)
	defer writer.Flush()
	
	// Записываем заголовок
	err = writer.Write([]string{"Min Price (USD)", "Max Price (USD)", "Country", "Currency", "Original Price Range"})
	if err != nil {
		return "", fmt.Errorf("Ошибка записи заголовка CSV: %v", err)
	}
	
	// Записываем данные
	for _, data := range priceData {
		err = writer.Write([]string{
			strconv.FormatFloat(data.MinPriceUSD, 'f', 2, 64),
			strconv.FormatFloat(data.MaxPriceUSD, 'f', 2, 64),
			data.CountryCode,
			data.CurrencyCode,
			data.OriginalPrice,
		})
		if err != nil {
			log.Printf("Ошибка записи строки в CSV: %v", err)
			continue // Продолжаем с остальными строками
		}
	}
	
	return filepath, nil
}

// Функция обработки команды start
func handleStart(update tgbotapi.Update, bot *tgbotapi.BotAPI) {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, 
		"Привет! Отправьте ссылку на приложение из Google Play "+
		"(формата https://play.google.com/store/apps/details?id=xxx).")
	bot.Send(msg)
}

// Функция обработки входящих сообщений
func handleMessage(update tgbotapi.Update, bot *tgbotapi.BotAPI) {
	startTime := time.Now()
	chatID := update.Message.Chat.ID
	text := update.Message.Text
	
	// Получаем информацию о пользователе
	user := update.Message.From
	username := user.UserName
	if username == "" {
		username = "неизвестный"
	}
	fullName := user.FirstName
	if user.LastName != "" {
		fullName += " " + user.LastName
	}
	
	// Проверяем, содержит ли сообщение ссылку на Google Play
	if strings.Contains(text, "play.google.com") {
		re := regexp.MustCompile(`id=([\w\d\.]+)`)
		matches := re.FindStringSubmatch(text)
		
		if len(matches) < 2 {
			msg := tgbotapi.NewMessage(chatID, "Не удалось найти идентификатор приложения в Google Play ссылке.")
			bot.Send(msg)
			return
		}
		
		appID := matches[1]
		filepath, err := fetchPricesGoogle(appID, chatID, bot)
		
		if err != nil {
			log.Printf("Ошибка при получении цен для %s: %v", appID, err)
			msg := tgbotapi.NewMessage(chatID, 
				fmt.Sprintf("Произошла ошибка при получении информации о ценах для %s: %v", appID, err))
			bot.Send(msg)
			return
		}
		
		if filepath != "" {
			file := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filepath))
			_, err := bot.Send(file)
			if err != nil {
				log.Printf("Ошибка при отправке файла: %v", err)
				msg := tgbotapi.NewMessage(chatID, "Произошла ошибка при отправке файла.")
				bot.Send(msg)
			}
		} else {
			msg := tgbotapi.NewMessage(chatID, 
				fmt.Sprintf("Информация о ценах для приложения %s не найдена или страница недоступна.", appID))
			bot.Send(msg)
		}
	} else {
		msg := tgbotapi.NewMessage(chatID, 
			"Ссылка не распознана. Отправьте ссылку на приложение Google Play.")
		bot.Send(msg)
	}
	
	// Записываем логи
	endTime := time.Now()
	totalTime := endTime.Sub(startTime).Seconds()
	log.Printf("Время выполнения: %.2f секунд.", totalTime)
	log.Printf("Запрос от %s (@%s)", fullName, username)
	
	// Записываем в файл логов
	logsPath := filepath.Join(CONST_PATH, "logs.log")
	file, err := os.OpenFile(logsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Ошибка при открытии файла логов: %v", err)
		return
	}
	defer file.Close()
	
	writer := csv.NewWriter(file)
	defer writer.Flush()
	
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	err = writer.Write([]string{
		currentTime,
		fmt.Sprintf("Время: %.2f сек, пользователь: %s (@%s)", totalTime, fullName, username),
	})
	if err != nil {
		log.Printf("Ошибка при записи логов: %v", err)
	}
}

func main() {
	// Создаем директорию для данных, если не существует
	if _, err := os.Stat(CONST_PATH); os.IsNotExist(err) {
		os.MkdirAll(CONST_PATH, 0755)
	}
	
	// Инициализируем бота
	bot, err := tgbotapi.NewBotAPI(CONST_TOKEN)
	if err != nil {
		log.Fatalf("Ошибка при создании бота: %v", err)
	}
	
	log.Printf("Авторизован как %s", bot.Self.UserName)
	
	// Настройка обновлений
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	
	updates := bot.GetUpdatesChan(u)
	
	// Обрабатываем сообщения
	for update := range updates {
		if update.Message == nil { // Игнорируем обновления без сообщений
			continue
		}
		
		if update.Message.IsCommand() && update.Message.Command() == "start" {
			go handleStart(update, bot)
		} else if update.Message.Text != "" {
			go handleMessage(update, bot)
		}
	}
}
