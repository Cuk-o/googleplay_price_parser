package main

import (
	"context"
	"encoding/csv"
	// "encoding/json"
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
	CONST_TOKEN       = "6993196254:AAG6Za3SDay3hNSrvneCGFlmg8vRn9W2PYs" // Замените на ваш токен бота
	CONST_PATH        = "./data"         // Путь для сохранения файлов
	CACHE_EXPIRY_HOURS = 24              // Срок действия кэша в часах
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
	"DZD": 132.966, "AUD": 1.583982, "BHD": 0.376241, "BDT": 109.73,
	"BOB": 6.909550, "BRL": 5.806974, "CAD": 1.433827, "KYD": 0.833,
	"CLP": 961.794638, "COP": 4153.599492, "CRC": 504.817577, "EGP": 50.581311,
	"GEL": 2.867107, "GHS": 15.187930, "HKD": 7.787505, "INR": 86.249922,
	"IDR": 16149.393463, "IQD": 1309.703222, "ILS": 3.587793, "JPY": 155.855438,
	"JOD": 0.709118, "KZT": 505.503277, "KES": 129.264801, "KRW": 1432.185253,
	"KWD": 0.308060, "MOP": 8.021963, "MYR": 4.392292, "MXN": 20.245294,
	"MAD": 10.007902, "MMK": 2099.980901, "NZD": 1.752597, "NGN": 1550.620034,
	"OMR": 0.384454, "PKR": 278.655722, "PYG": 7918.619687, "PEN": 3.712514,
	"PHP": 58.388686, "QAR": 3.639992, "RUB": 97.929483, "SAR": 3.750482,
	"RSD": 112.125584, "SGD": 1.348339, "ZAR": 18.384263, "LKR": 298.761937,
	"TWD": 32.687009, "TZS": 2507.601986, "THB": 33.712166, "TRY": 35.678472,
	"UAH": 40.939132, "AED": 3.671703, "USD": 1, "VND": 25094.287781,
}

// Структура для хранения найденных цен
type PriceData struct {
	MinPriceUSD   float64
	MaxPriceUSD   float64
	CountryCode   string
	CurrencyCode  string
	OriginalPrice string
}

// Структура для кэша
type CacheEntry struct {
	Timestamp time.Time
	filePath   string
}

// Кэш для хранения результатов
var priceCache = make(map[string]CacheEntry)
var cacheMutex sync.RWMutex

// Структура для ограничения скорости запросов
type RateLimiter struct {
	rate       float64
	per        float64
	allowance  float64
	lastUpdate time.Time
	mu         sync.Mutex
}

// Создает новый ограничитель скорости
func NewRateLimiter(rate, per float64) *RateLimiter {
	return &RateLimiter{
		rate:       rate,
		per:        per,
		allowance:  rate,
		lastUpdate: time.Now(),
	}
}

// Проверяет, можно ли выполнить запрос
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	current := time.Now()
	timePassed := current.Sub(rl.lastUpdate).Seconds()
	rl.lastUpdate = current
	
	rl.allowance += timePassed * (rl.rate / rl.per)
	if rl.allowance > rl.rate {
		rl.allowance = rl.rate // предел
	}
	
	if rl.allowance < 1.0 {
		return false
	}
	
	rl.allowance -= 1.0
	return true
}

// Ожидает, пока можно будет выполнить запрос
func (rl *RateLimiter) Wait() {
	for {
		rl.mu.Lock()
		current := time.Now()
		timePassed := current.Sub(rl.lastUpdate).Seconds()
		rl.lastUpdate = current
		
		rl.allowance += timePassed * (rl.rate / rl.per)
		if rl.allowance > rl.rate {
			rl.allowance = rl.rate
		}
		
		if rl.allowance < 1.0 {
			waitTime := (1.0 - rl.allowance) * rl.per / rl.rate
			rl.mu.Unlock()
			time.Sleep(time.Duration(waitTime * float64(time.Second)))
		} else {
			rl.allowance -= 1.0
			rl.mu.Unlock()
			return
		}
	}
}

// Глобальный ограничитель скорости запросов
var googleLimiter = NewRateLimiter(10.0, 1.0) // 10 запросов в секунду

// Структура для хранения данных о запросах пользователей
type UserRequestData struct {
	RequestTimes []time.Time
	mu           sync.Mutex
}

// Карта запросов пользователей для ограничения скорости
var userRequests = make(map[int64]*UserRequestData)
var userRequestsMu sync.Mutex

// Проверяет, не превышен ли лимит запросов пользователя
func checkUserRateLimit(userID int64, maxRequests int, periodSeconds int) bool {
	userRequestsMu.Lock()
	if _, exists := userRequests[userID]; !exists {
		userRequests[userID] = &UserRequestData{
			RequestTimes: []time.Time{},
		}
	}
	data := userRequests[userID]
	userRequestsMu.Unlock()
	
	data.mu.Lock()
	defer data.mu.Unlock()
	
	now := time.Now()
	cutoff := now.Add(-time.Duration(periodSeconds) * time.Second)
	
	// Очищаем старые запросы
	newTimes := []time.Time{}
	for _, t := range data.RequestTimes {
		if t.After(cutoff) {
			newTimes = append(newTimes, t)
		}
	}
	data.RequestTimes = newTimes
	
	// Проверяем, не превышен ли лимит
	if len(data.RequestTimes) >= maxRequests {
		return false
	}
	
	// Добавляем текущий запрос
	data.RequestTimes = append(data.RequestTimes, now)
	return true
}

// Функция для получения данных из кэша
func getFromCache(appID string, storeType string) string {
	cacheKey := fmt.Sprintf("%s_%s", appID, storeType)
	
	cacheMutex.RLock()
	defer cacheMutex.RUnlock()
	
	if entry, exists := priceCache[cacheKey]; exists {
		if time.Since(entry.Timestamp) < time.Duration(CACHE_EXPIRY_HOURS)*time.Hour && fileExists(entry.filePath ) {
			log.Printf("Кэш-хит для %s", cacheKey)
			return entry.filePath
		}
	}
	
	return ""
}

// Функция для сохранения данных в кэш
func saveToCache(appID string, storeType string, filepath string) {
	cacheKey := fmt.Sprintf("%s_%s", appID, storeType)
	
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	
	priceCache[cacheKey] = CacheEntry{
		Timestamp: time.Now(),
		filePath:  filepath, // Исправлено: используем параметр функции
	}
	
	log.Printf("Сохранено в кэш: %s", cacheKey)
}

// Проверяет, существует ли файл
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
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
	case "COP":
		priceStr = strings.ReplaceAll(priceStr, "COP ", "")
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
		// Случай, когда в строке есть и точки, и запятые (например, "1,040,000.00")
		dotIndex := strings.LastIndex(matches, ".")
		commaIndex := strings.LastIndex(matches, ",")
		
		if dotIndex > commaIndex {
			// Если последняя точка после последней запятой, 
			// то точка - разделитель дробной части, а запятые - разделители тысяч
			// (Формат: "1,234,567.89")
			numStr = strings.ReplaceAll(matches, ",", "")
		} else {
			// Если последняя запятая после последней точки,
			// то запятая - разделитель дробной части, а точки - разделители тысяч
			// (Формат: "1.234.567,89")
			numStr = strings.ReplaceAll(matches, ".", "")
			numStr = strings.ReplaceAll(numStr, ",", ".")
		}
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

// Функция для повторного выполнения HTTP-запроса с экспоненциальной задержкой
func getWithRetry(url string, maxRetries int) (string, int, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
	}
	
	var content string
	var statusCode int
	var err error
	
	backoffFactor := 1.5
	
	for i := 0; i < maxRetries; i++ {
		// Ожидаем разрешения от ограничителя скорости
		googleLimiter.Wait()
		
		resp, err := client.Get(url)
		if err != nil {
			log.Printf("Попытка %d из %d: ошибка запроса %s: %v", i+1, maxRetries, url, err)
			if i == maxRetries-1 {
				return "", 0, err
			}
			waitTime := time.Duration(backoffFactor * math.Pow(2, float64(i))) * time.Second
			time.Sleep(waitTime)
			continue
		}
		defer resp.Body.Close()
		
		statusCode = resp.StatusCode
		if statusCode != http.StatusOK {
			log.Printf("Попытка %d из %d: HTTP-статус %d для %s", i+1, maxRetries, statusCode, url)
			if statusCode == http.StatusNotFound {
				return "", http.StatusNotFound, nil
			}
			if i == maxRetries-1 {
				return "", statusCode, fmt.Errorf("Не удалось получить успешный ответ после %d попыток", maxRetries)
			}
			waitTime := time.Duration(backoffFactor * math.Pow(2, float64(i))) * time.Second
			time.Sleep(waitTime)
			continue
		}
		
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Попытка %d из %d: ошибка чтения ответа для %s: %v", i+1, maxRetries, url, err)
			if i == maxRetries-1 {
				return "", statusCode, err
			}
			waitTime := time.Duration(backoffFactor * math.Pow(2, float64(i))) * time.Second
			time.Sleep(waitTime)
			continue
		}
		
		content = string(bodyBytes)
		return content, statusCode, nil
	}
	
	return content, statusCode, err
}

// Функция для получения цен для конкретной страны
func getPricesForCountry(countryCode string, appID string) ([]string, string, error) {
	currencyCode, exists := countryCurrencyDict[countryCode]
	if !exists {
		currencyCode = "USD"
	}
	
	url := fmt.Sprintf("https://play.google.com/store/apps/details?id=%s&hl=en&gl=%s", appID, countryCode)
	
	content, statusCode, err := getWithRetry(url, 3)
	if err != nil {
		return nil, currencyCode, fmt.Errorf("Ошибка запроса после повторных попыток: %v", err)
	}
	
	if statusCode == http.StatusNotFound {
		return nil, currencyCode, fmt.Errorf("404")
	}
	
	if content == "" {
		return nil, currencyCode, fmt.Errorf("Пустой ответ")
	}
	
	log.Printf("[Google] %s", countryCode)
	
	// Проверяем наличие ошибки 404
	if strings.Contains(content, "We're sorry, the requested URL was not found on this server.") {
		return nil, currencyCode, fmt.Errorf("404")
	}
	
	// Проверяем наличие покупок в приложении
	if !strings.Contains(content, "In-app purchases") {
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
		matches := re.FindAllStringSubmatch(content, -1)
		
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
		matches := rePrice.FindAllStringSubmatch(content, -1)
		
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
	// Проверяем кэш
	cachedPath := getFromCache(appID, "google")
	if cachedPath != "" {
		msg := tgbotapi.NewMessage(chatID, "Возвращаем данные из кэша...")
		bot.Send(msg)
		return cachedPath, nil
	}
	
	msg := tgbotapi.NewMessage(chatID, "Обработка для Google Play началась...")
	progressMsg, err := bot.Send(msg)
	if err != nil {
		return "", fmt.Errorf("Ошибка отправки сообщения: %v", err)
	}
	
	var priceData []PriceData
	var mu sync.Mutex // Для безопасной записи в общий слайс
	
	// Используем errgroup для параллельной обработки
	g, ctx := errgroup.WithContext(context.Background())
	sem := make(chan struct{}, 5) // Ограничиваем количество параллельных запросов
	
	// Общее количество стран и групп
	totalCountries := len(countries)
	batchSize := 5
	totalBatches := (totalCountries + batchSize - 1) / batchSize
	
	for batchNum := 0; batchNum < totalBatches; batchNum++ {
		// Обновляем сообщение о прогрессе
		progressPercent := int(float64(batchNum) / float64(totalBatches) * 100)
		progressText := fmt.Sprintf("Прогресс: %d%% (%d/%d групп стран)", progressPercent, batchNum, totalBatches)
		
		progressEdit := tgbotapi.NewEditMessageText(chatID, progressMsg.MessageID, progressText)
		bot.Send(progressEdit)
		
		// Обрабатываем страны в текущей группе
		start := batchNum * batchSize
		end := start + batchSize
		if end > totalCountries {
			end = totalCountries
		}
		
		for i := start; i < end; i++ {
			countryCode := countries[i]
			
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
		
		// Ждем завершения всех горутин в текущей группе
		if err := g.Wait(); err != nil {
			log.Printf("Предупреждение: возникла ошибка в одной из горутин: %v", err)
			// Продолжаем выполнение с теми данными, которые удалось получить
		}
		
		// Сбрасываем группу для следующей партии
		g, ctx = errgroup.WithContext(context.Background())
	}
	
	// Проверяем, есть ли данные
	if len(priceData) == 0 {
		return "", fmt.Errorf("Не удалось получить данные о ценах ни для одной страны")
	}
	
	// Обновляем сообщение о статусе обработки
	statusMsg := fmt.Sprintf("Обработка завершена. Получена информация о ценах для %d из %d стран.", len(priceData), len(countries))
	finalEdit := tgbotapi.NewEditMessageText(chatID, progressMsg.MessageID, statusMsg)
	bot.Send(finalEdit)
	
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
	
	// Создаем уникальное имя файла с временной меткой
	timestamp := time.Now().Format("20060102_150405")
	filepath := filepath.Join(CONST_PATH, fmt.Sprintf("%s_google_%s.csv", appID, timestamp))
	
	// Создаем CSV-файл
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
	
	// Сохраняем в кэш
	saveToCache(appID, "google", filepath)
	
	return filepath, nil
}

// Функция валидации URL-адреса приложения
func validateAppURL(url string) (string, string) {
	if strings.Contains(url, "play.google.com") {
		re := regexp.MustCompile(`id=([\w\d\.]+)`)
		matches := re.FindStringSubmatch(url)
		if len(matches) >= 2 {
			return "google", matches[1]
		}
	}
	return "", ""
}

// Функция обработки команды start
func handleStart(update tgbotapi.Update, bot *tgbotapi.BotAPI) {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, 
		"👋 Привет! Я бот для анализа цен приложений.\n\n"+
		"Отправьте мне ссылку на приложение из:\n"+
		"• Google Play (https://play.google.com/store/apps/details?id=xxx)\n\n"+
		"Я проанализирую цены в разных странах и пришлю вам отчет в CSV формате.")
	bot.Send(msg)
}

// Функция обработки команды help
func handleHelp(update tgbotapi.Update, bot *tgbotapi.BotAPI) {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"📚 Справка по использованию бота:\n\n"+
		"1️⃣ Отправьте ссылку на приложение из Google Play\n"+
		"2️⃣ Дождитесь обработки (это может занять несколько минут)\n"+
		"3️⃣ Получите CSV-файл с анализом цен\n\n"+
		"Примеры ссылок:\n"+
		"• https://play.google.com/store/apps/details?id=com.example.app\n\n"+
		"Для повторного запуска бота используйте команду /start")
	bot.Send(msg)
}

// Функция обработки команды status
func handleStatus(update tgbotapi.Update, bot *tgbotapi.BotAPI) {
	cacheSize := 0
	cacheMutex.RLock()
	cacheSize = len(priceCache)
	cacheMutex.RUnlock()
	
	// userID := update.Message.From.ID
	
	// Получаем информацию о лимите запросов пользователя
	recentRequests := 0
	userRequestsMu.Lock()
	if data, exists := userRequests[update.Message.Chat.ID]; exists {
		data.mu.Lock()
		now := time.Now()
		for _, t := range data.RequestTimes {
			if now.Sub(t) < time.Minute {
				recentRequests++
			}
		}
		data.mu.Unlock()
	}
	userRequestsMu.Unlock()
	
	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"📊 Статус системы:\n\n"+
		fmt.Sprintf("• Размер кэша: %d записей\n", cacheSize)+
		fmt.Sprintf("• Ваши запросы: %d/5 за последнюю минуту\n", recentRequests)+
		fmt.Sprintf("• Время сервера: %s", time.Now().Format("2006-01-02 15:04:05")))
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
	// userID := user.ID
	fullName := user.FirstName
	if user.LastName != "" {
		fullName += " " + user.LastName
	}
	
	// Проверяем ограничение скорости пользователя
	if !checkUserRateLimit(chatID, 5, 60) {
		msg := tgbotapi.NewMessage(chatID, "⏳ Вы превысили лимит запросов. Попробуйте снова через минуту.")
		bot.Send(msg)
		return
	}
	
	// Валидация ссылки
	storeType, appID := validateAppURL(text)
	if appID == "" {
		msg := tgbotapi.NewMessage(chatID, "❌ Неверная ссылка. Пожалуйста, отправьте ссылку на приложение из Google Play.")
		bot.Send(msg)
		return
	}
	
	// Обработка ссылки
	var filePath string
	var err error
	
	if storeType == "google" {
		filePath, err = fetchPricesGoogle(appID, chatID, bot)
		if err != nil {
			log.Printf("Ошибка при получении цен для %s: %v", appID, err)
			msg := tgbotapi.NewMessage(chatID, 
				fmt.Sprintf("❌ Произошла ошибка при получении информации о ценах: %v", err))
			bot.Send(msg)
			return
		}
	} else {
		msg := tgbotapi.NewMessage(chatID, "❌ Неподдерживаемый тип магазина. В настоящее время поддерживается только Google Play.")
		bot.Send(msg)
		return
	}
	
	// Проверяем наличие файла
	if filePath != "" && fileExists(filePath) {
		file := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
		file.Caption = "📄 Ваш отчет готов!"
		_, err := bot.Send(file)
		if err != nil {
			log.Printf("Ошибка при отправке файла: %v", err)
			msg := tgbotapi.NewMessage(chatID, "❌ Произошла ошибка при отправке файла.")
			bot.Send(msg)
		}
	} else {
		msg := tgbotapi.NewMessage(chatID, 
			fmt.Sprintf("❌ Информация о ценах для приложения %s не найдена или страница недоступна.", appID))
		bot.Send(msg)
	}
	
	// Записываем логи
	endTime := time.Now()
	totalTime := endTime.Sub(startTime).Seconds()
	log.Printf("Время выполнения: %.2f секунд.", totalTime)
	log.Printf("Запрос от %s (@%s)", fullName, username)
	
	// Записываем в файл логов
	logsPath := filepath.Join(CONST_PATH, "logs.log") // Исправлено: используем пакет filepath
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
		fmt.Sprintf("Время: %.2f сек, пользователь: %s (@%s), appID: %s", totalTime, fullName, username, appID),
	})
	if err != nil {
		log.Printf("Ошибка при записи логов: %v", err)
	}
}

// Инициализация и настройка системы логирования
func initLogging() {
	// Создаем директорию для логов, если не существует
	if _, err := os.Stat(CONST_PATH); os.IsNotExist(err) {
		os.MkdirAll(CONST_PATH, 0755)
	}
	
	// Открываем файл логов
	logFile, err := os.OpenFile(
		filepath.Join(CONST_PATH, "app.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		log.Fatalf("Не удалось открыть файл логов: %v", err)
	}
	
	// Настраиваем вывод логов в файл и в консоль
	log.SetOutput(io.MultiWriter(logFile, os.Stdout))
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	
	log.Println("Логирование инициализировано")
}

func main() {
	// Инициализируем логирование
	initLogging()
	
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
		
		if update.Message.IsCommand() {
			// Обрабатываем команды
			switch update.Message.Command() {
			case "start":
				go handleStart(update, bot)
			case "help":
				go handleHelp(update, bot)
			case "status":
				go handleStatus(update, bot)
			}
		} else if update.Message.Text != "" {
			go handleMessage(update, bot)
		}
	}
}
