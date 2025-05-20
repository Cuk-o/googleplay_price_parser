package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
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
	"unicode"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"golang.org/x/sync/errgroup"
)

// Конфигурация
const (
	CONST_TOKEN        = ""              // <-- ВСТАВЬТЕ СЮДА СВОЙ ТОКЕН!
	CONST_PATH         = "./data"             // Путь для сохранения файлов
	CACHE_EXPIRY_HOURS = 24                   // Срок действия кэша в часах
	RATES_PATH         = "./data/usd_rates.json"
	CSV_MAX_AGE        = 72 * time.Hour // 3 дня
)

// Словарь для соответствия страны и валюты
var countryCurrencyDict = map[string]string{
	"DZ": "DZD", "AU": "AUD", "BH": "BHD", "BD": "BDT", "BO": "BOB", "BR": "BRL",
	"KH": "KHR", "CA": "CAD", "KY": "KYD", "CL": "CLP", "CO": "COP", "CR": "CRC",
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

// Глобальные переменные для курса
var currencyRates = map[string]float64{}
var usdToRubRate float64 = 100.0
var lastRatesUpdate time.Time
var currencyRatesFromAPI map[string]float64

// Структура для хранения найденных цен
type PriceData struct {
	MinPriceUSD   float64
	MaxPriceUSD   float64
	CountryCode   string
	CurrencyCode  string
	OriginalPrice string
	MaxPriceRUB   float64
}

// Структура для кэша
type CacheEntry struct {
	Timestamp time.Time
	filePath  string
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

func removeAllSpaces(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
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
		rl.allowance = rl.rate
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

// === КУРСЫ ВАЛЮТ С CDN ===
func updateCurrencyRates() error {
	stat, err := os.Stat(RATES_PATH)
	if err == nil && time.Since(stat.ModTime()) < 24*time.Hour {
		data, err := os.ReadFile(RATES_PATH)
		if err == nil {
			var jsonData struct {
				USD map[string]float64 `json:"usd"`
			}
			if err := json.Unmarshal(data, &jsonData); err == nil {
				currencyRatesFromAPI = jsonData.USD
				usdToRubRate = currencyRatesFromAPI["rub"]
				lastRatesUpdate = stat.ModTime()
				for k, v := range currencyRatesFromAPI {
					currencyRates[strings.ToUpper(k)] = v
				}
				return nil
			}
		}
	}
	resp, err := http.Get("https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/usd.json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	_ = os.WriteFile(RATES_PATH, body, 0644)
	var jsonData struct {
		USD map[string]float64 `json:"usd"`
	}
	if err := json.Unmarshal(body, &jsonData); err != nil {
		return err
	}
	currencyRatesFromAPI = jsonData.USD
	usdToRubRate = currencyRatesFromAPI["rub"]
	lastRatesUpdate = time.Now()
	for k, v := range currencyRatesFromAPI {
		currencyRates[strings.ToUpper(k)] = v
	}
	return nil
}

func convertUSDToRUB(usd float64) float64 {
	return round(usd*usdToRubRate, 2)
}

// === АВТО-ОЧИСТКА СТАРЫХ CSV ===
func cleanupOldCSVs(dir string, maxAge time.Duration) {
	files, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("Ошибка при сканировании директории: %v", err)
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, file := range files {
		if file.Type().IsRegular() && strings.HasSuffix(file.Name(), ".csv") {
			info, err := file.Info()
			if err == nil && info.ModTime().Before(cutoff) {
				path := filepath.Join(dir, file.Name())
				if err := os.Remove(path); err == nil {
					log.Printf("Удален старый CSV: %s", path)
				}
			}
		}
	}
}

// Функция для получения данных из кэша
func getFromCache(appID string, storeType string) string {
	cacheKey := fmt.Sprintf("%s_%s", appID, storeType)

	cacheMutex.RLock()
	defer cacheMutex.RUnlock()

	if entry, exists := priceCache[cacheKey]; exists {
		if time.Since(entry.Timestamp) < time.Duration(CACHE_EXPIRY_HOURS)*time.Hour && fileExists(entry.filePath) {
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
		filePath:  filepath,
	}

	log.Printf("Сохранено в кэш: %s", cacheKey)
}

// Проверяет, существует ли файл
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// Функция для округления числа до заданного количества знаков после запятой
func round(num float64, precision int) float64 {
	mult := math.Pow10(precision)
	return math.Round(num*mult) / mult
}

// Функция для парсинга цены в зависимости от валюты
func parsePrice(priceStr string, currencyCode string) (float64, error) {
	priceStr = removeAllSpaces(priceStr)	// Очищаем от валютных символов
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
		priceStr = strings.ReplaceAll(priceStr, "R", "")
		priceStr = strings.ReplaceAll(priceStr, " ", "")
	case "COP":
		priceStr = strings.ReplaceAll(priceStr, "COP ", "")
	}


	// Паттерн для цены: ищем числа, где возможен разделитель тысяч (пробел или неразрывный пробел), 
	// десятичный разделитель — запятая или точка.
	re := regexp.MustCompile(`[\d.,]+`)
	match := re.FindString(priceStr)
	if match == "" {
		return 0, fmt.Errorf("Не найдено число в строке: %s", priceStr)
	}

	// Преобразуем: 
	// если есть запятая и точка — определяем, что запятая = тысячный, точка = десятичный
	// если только запятая — вероятно, это десятичный разделитель, а пробел — тысячный

	dotCount := strings.Count(match, ".")
	commaCount := strings.Count(match, ",")

	if dotCount > 1 && commaCount == 0 {
		// Пример: "2.150.000" → "2150000"
		match = strings.ReplaceAll(match, ".", "")
	} else if commaCount > 1 && dotCount == 0 {
		// Пример: "2,150,000" → "2150000"
		match = strings.ReplaceAll(match, ",", "")
	} else if dotCount > 0 && commaCount > 0 {
		lastDot := strings.LastIndex(match, ".")
		lastComma := strings.LastIndex(match, ",")
		if lastComma > lastDot {
			// "2.150.000,00" → "2150000.00"
			match = strings.ReplaceAll(match, ".", "")
			match = strings.ReplaceAll(match, ",", ".")
		} else {
			// "2,150,000.00" → "2150000.00"
			match = strings.ReplaceAll(match, ",", "")
		}
	} else if commaCount == 1 && dotCount == 0 {
		// Если после запятой ровно 3 цифры — это тысячный разделитель, иначе десятичный
		parts := strings.Split(match, ",")
		if len(parts[1]) == 3 {
			// "2,000" → "2000"
			match = strings.ReplaceAll(match, ",", "")
		} else {
			// "2150,00" → "2150.00"
			match = strings.ReplaceAll(match, ",", ".")
		}
	}
	// Если осталась запятая — это десятичный разделитель, а точка — тысячный
	// else: точка только одна — стандартный float


	value, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0, fmt.Errorf("Ошибка парсинга float из '%s': %v", match, err)
	}
	return value, nil
}


// Функция для конвертации строки цены в USD
func convertPriceToUSD(priceStr string, currencyCode string) (float64, float64, error) {
	currencyCode = strings.ToUpper(currencyCode)
	priceStr = strings.TrimSpace(priceStr)
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
		rangeParts := strings.Split(firstRange, "-")
		if len(rangeParts) < 2 {
			return 0, 0, fmt.Errorf("Некорректный формат диапазона цен: %s", firstRange)
		}

		minPriceStr := strings.TrimSpace(rangeParts[0])
		maxPriceStr := strings.TrimSpace(rangeParts[1])

		if minPriceStr == "" || maxPriceStr == "" {
			return 0, 0, fmt.Errorf("Пустая строка в диапазоне цен: %s", firstRange)
		}

		minPrice, err = parsePrice(minPriceStr, currencyCode)
		if err != nil {
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

		maxPrice, err = parsePrice(maxPriceStr, currencyCode)
		if err != nil {
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
		minPrice, err = parsePrice(firstRange, currencyCode)
		if err != nil {
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

	// получаем курс валюты: сколько одной валюты за 1 USD
	rate, exists := currencyRates[currencyCode]
	if !exists || rate == 0 {
		rate = 1.0
	}

	// rate = "сколько валюты за 1 USD"
	// Значит, 1 единица валюты = 1/rate USD
	// То есть, чтобы перевести валюту в USD: price * (1 / rate)
	minUSD := math.Max(round(minPrice * (1 / rate), 2), 0.01)
	maxUSD := math.Max(round(maxPrice * (1 / rate), 2), 0.01)


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

	if strings.Contains(content, "We're sorry, the requested URL was not found on this server.") {
		return nil, currencyCode, fmt.Errorf("404")
	}

	if !strings.Contains(content, "In-app purchases") {
		return nil, currencyCode, fmt.Errorf("На странице нет текста 'In-app purchases'")
	}

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

		if len(prices) > 0 {
			break
		}
	}

	if len(prices) == 0 {
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

// === ИЗМЕНЁННАЯ ФУНКЦИЯ fetchPricesGoogle ===
func fetchPricesGoogle(appID string, chatID int64, bot *tgbotapi.BotAPI) (string, error) {
	if err := updateCurrencyRates(); err != nil {
		log.Printf("Не удалось обновить курсы валют: %v", err)
	}

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
	var mu sync.Mutex

	var successCount, failCount int

	g, ctx := errgroup.WithContext(context.Background())
	sem := make(chan struct{}, 5)
	totalCountries := len(countries)
	batchSize := 5
	totalBatches := (totalCountries + batchSize - 1) / batchSize

	for batchNum := 0; batchNum < totalBatches; batchNum++ {
		progressPercent := int(float64(batchNum) / float64(totalBatches) * 100)
		progressText := fmt.Sprintf(
			"Прогресс: %d%% (%d/%d групп стран)\nУспех: %d | Ошибка: %d",
			progressPercent, batchNum, totalBatches, successCount, failCount,
		)
		progressEdit := tgbotapi.NewEditMessageText(chatID, progressMsg.MessageID, progressText)
		bot.Send(progressEdit)

		start := batchNum * batchSize
		end := start + batchSize
		if end > totalCountries {
			end = totalCountries
		}

		for i := start; i < end; i++ {
			countryCode := countries[i]
			select {
			case <-ctx.Done():
				break
			default:
				sem <- struct{}{}
				g.Go(func() error {
					defer func() { <-sem }()
					defer func() {
						if r := recover(); r != nil {
							log.Printf("[Google] %s: Восстановление после паники: %v", countryCode, r)
						}
					}()
					prices, currencyCode, err := getPricesForCountry(countryCode, appID)
					if err != nil {
						log.Printf("[Google] %s: %v", countryCode, err)
						mu.Lock()
						failCount++
						mu.Unlock()
						return nil
					}
					if len(prices) > 0 {
						minPriceUSD, maxPriceUSD, err := convertPriceToUSD(prices[0], currencyCode)
						if err != nil {
							log.Printf("Ошибка при конвертации цены для %s: %v", countryCode, err)
							mu.Lock()
							failCount++
							mu.Unlock()
							return nil
						}
						mu.Lock()
						priceData = append(priceData, PriceData{
							MinPriceUSD:   minPriceUSD,
							MaxPriceUSD:   maxPriceUSD,
							CountryCode:   countryCode,
							CurrencyCode:  currencyCode,
							OriginalPrice: prices[0],
							MaxPriceRUB:   convertUSDToRUB(maxPriceUSD),
						})
						successCount++
						mu.Unlock()
					}
					return nil
				})
			}
		}
		if err := g.Wait(); err != nil {
			log.Printf("Предупреждение: возникла ошибка в одной из горутин: %v", err)
		}
		g, ctx = errgroup.WithContext(context.Background())
	}

	if len(priceData) == 0 {
		return "", fmt.Errorf("Не удалось получить данные о ценах ни для одной страны")
	}

	statusMsg := fmt.Sprintf(
		"Обработка завершена. Получена информация о ценах для %d из %d стран.\nОшибок: %d.",
		successCount, len(countries), failCount,
	)
	finalEdit := tgbotapi.NewEditMessageText(chatID, progressMsg.MessageID, statusMsg)
	bot.Send(finalEdit)

	sort.Slice(priceData, func(i, j int) bool {
		return priceData[i].MaxPriceUSD < priceData[j].MaxPriceUSD
	})

	if _, err := os.Stat(CONST_PATH); os.IsNotExist(err) {
		err := os.MkdirAll(CONST_PATH, 0755)
		if err != nil {
			return "", fmt.Errorf("Ошибка создания директории: %v", err)
		}
	}

	timestamp := time.Now().Format("20060102_150405")
	fpath := filepath.Join(CONST_PATH, fmt.Sprintf("%s_google_%s.csv", appID, timestamp))
	file, err := os.Create(fpath)
	if err != nil {
		return "", fmt.Errorf("Ошибка создания файла: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	err = writer.Write([]string{"Min Price (USD)", "Max Price (USD)", "Max Price (RUB)", "Country", "Currency", "Original Price Range"})
	if err != nil {
		return "", fmt.Errorf("Ошибка записи заголовка CSV: %v", err)
	}
	for _, data := range priceData {
		err = writer.Write([]string{
			strconv.FormatFloat(data.MinPriceUSD, 'f', 2, 64),
			strconv.FormatFloat(data.MaxPriceUSD, 'f', 2, 64),
			strconv.FormatFloat(data.MaxPriceRUB, 'f', 2, 64),
			data.CountryCode,
			data.CurrencyCode,
			data.OriginalPrice,
		})
		if err != nil {
			log.Printf("Ошибка записи строки в CSV: %v", err)
			continue
		}
	}

	saveToCache(appID, "google", fpath)
	cleanupOldCSVs(CONST_PATH, CSV_MAX_AGE)
	return fpath, nil
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
		"👋 Привет! Я бот для анализа цен приложений.\n\n" +
			"Отправьте мне ссылку на приложение из:\n" +
			"• Google Play (https://play.google.com/store/apps/details?id=xxx)\n\n" +
			"Я проанализирую цены в разных странах и пришлю вам отчет в CSV формате.")
	bot.Send(msg)
}

// Функция обработки команды help
func handleHelp(update tgbotapi.Update, bot *tgbotapi.BotAPI) {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
		"📚 Справка по использованию бота:\n\n" +
			"1️⃣ Отправьте ссылку на приложение из Google Play\n" +
			"2️⃣ Дождитесь обработки (это может занять несколько минут)\n" +
			"3️⃣ Получите CSV-файл с анализом цен\n\n" +
			"Примеры ссылок:\n" +
			"• https://play.google.com/store/apps/details?id=com.example.app\n\n" +
			"Для повторного запуска бота используйте команду /start")
	bot.Send(msg)
}

// Функция обработки команды status
func handleStatus(update tgbotapi.Update, bot *tgbotapi.BotAPI) {
	cacheSize := 0
	cacheMutex.RLock()
	cacheSize = len(priceCache)
	cacheMutex.RUnlock()

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
		"📊 Статус системы:\n\n" +
			fmt.Sprintf("• Размер кэша: %d записей\n", cacheSize) +
			fmt.Sprintf("• Ваши запросы: %d/5 за последнюю минуту\n", recentRequests) +
			fmt.Sprintf("• Время сервера: %s", time.Now().Format("2006-01-02 15:04:05")))
	bot.Send(msg)
}

// Функция обработки входящих сообщений
func handleMessage(update tgbotapi.Update, bot *tgbotapi.BotAPI) {
	startTime := time.Now()
	chatID := update.Message.Chat.ID
	text := update.Message.Text

	user := update.Message.From
	username := user.UserName
	if username == "" {
		username = "неизвестный"
	}
	fullName := user.FirstName
	if user.LastName != "" {
		fullName += " " + user.LastName
	}

	if !checkUserRateLimit(chatID, 5, 60) {
		msg := tgbotapi.NewMessage(chatID, "⏳ Вы превысили лимит запросов. Попробуйте снова через минуту.")
		bot.Send(msg)
		return
	}

	storeType, appID := validateAppURL(text)
	if appID == "" {
		msg := tgbotapi.NewMessage(chatID, "❌ Неверная ссылка. Пожалуйста, отправьте ссылку на приложение из Google Play.")
		bot.Send(msg)
		return
	}

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

	endTime := time.Now()
	totalTime := endTime.Sub(startTime).Seconds()
	log.Printf("Время выполнения: %.2f секунд.", totalTime)
	log.Printf("Запрос от %s (@%s)", fullName, username)

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
		fmt.Sprintf("Время: %.2f сек, пользователь: %s (@%s), appID: %s", totalTime, fullName, username, appID),
	})
	if err != nil {
		log.Printf("Ошибка при записи логов: %v", err)
	}
}

// Инициализация и настройка системы логирования
func initLogging() {
	if _, err := os.Stat(CONST_PATH); os.IsNotExist(err) {
		os.MkdirAll(CONST_PATH, 0755)
	}
	logFile, err := os.OpenFile(
		filepath.Join(CONST_PATH, "app.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		log.Fatalf("Не удалось открыть файл логов: %v", err)
	}
	log.SetOutput(io.MultiWriter(logFile, os.Stdout))
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("Логирование инициализировано")
}

func main() {
	initLogging()
	if _, err := os.Stat(CONST_PATH); os.IsNotExist(err) {
		os.MkdirAll(CONST_PATH, 0755)
	}

	bot, err := tgbotapi.NewBotAPI(CONST_TOKEN)
	if err != nil {
		log.Fatalf("Ошибка при создании бота: %v", err)
	}

	log.Printf("Авторизован как %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}
		if update.Message.IsCommand() {
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
