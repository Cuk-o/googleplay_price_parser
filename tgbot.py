from telegram import Update, ForceReply
from telegram.ext import Updater, CommandHandler, MessageHandler, Filters, CallbackContext
import logging
import csv
import re
from selenium import webdriver
from selenium.webdriver.chrome.options import Options
import time
from selenium.webdriver.chrome.service import Service
from webdriver_manager.chrome import ChromeDriverManager

currency_rates = {
    "DZD": 134.55, "AUD": 1.53, "BHD": 0.376, "BDT": 109.73, "BOB": 6.91,
    "BRL": 4.97,  "CAD": 1.35, "KYD": 0.833, "CLP": 970.01,
    "COP": 3902.4, "CRC": 515.06, "EGP": 30.9, "GEL": 2.64, "GHS": 12.49,
    "HKD": 7.82, "INR": 83.05, "IDR": 15646.73, "IQD": 1309.63, "ILS": 3.61,
    "JPY": 150.2, "JOD": 0.709, "KZT": 450.04, "KES": 146.9, "KRW": 1332.97,
    "KWD": 0.308, "MOP": 8.06, "MYR": 4.78, "MXN": 17.06, "MAD": 10.06,
    "MMK": 2094.53, "NZD": 1.63, "NGN": 1505.32, "OMR": 0.384, "PKR": 279.29,
    "PYG": 7298.36, "PEN": 3.84, "PHP": 55.94, "QAR": 3.64, "RUB": 92.12,
    "SAR": 3.75, "RSD": 108.77, "SGD": 1.35, "ZAR": 18.88, "LKR": 312.29,
    "TWD": 31.33, "TZS": 2541.15, "THB": 36, "TRY": 30.87, "UAH": 37.96,
    "AED": 3.67, "USD": 1, "VND": 24526.38
}


# Настройка логирования
logging.basicConfig(format='%(asctime)s - %(name)s - %(levelname)s - %(message)s', level=logging.INFO)
logger = logging.getLogger(__name__)

def convert_price_to_usd(price_str, currency_code):
    price_match = re.search(r"(\d[\d,.]*)", price_str.replace(",", ""))
    if price_match:
        price = float(price_match.group(1).replace(",", ""))
        rate = currency_rates.get(currency_code.upper(), 1)  # По умолчанию 1, если валюта не найдена
        if currency_code == 'IDR':
            return round(price / rate / 100, 2)
        else:    
            return round(price / rate, 2)  # Конвертация в USD и округление
    return 0

def get_prices_for_country(driver, country_code, country_currency_dict, app_id):
    """
    Получает цены для страны по указанному идентификатору приложения.

    :param driver: Драйвер браузера.
    :param country_code: Код страны.
    :param country_currency_dict: Словарь с соответствием кода страны и валюты.
    :param app_id: Идентификатор приложения на Google Play.
    :return: Список найденных цен и код валюты.
    """
    # Получаем код валюты для страны, по умолчанию USD
    currency_code = country_currency_dict.get(country_code, "USD")
    
    # Формируем URL для доступа к странице приложения в Google Play
    url = f'https://play.google.com/store/apps/details?id={app_id}&hl=en_US&gl={country_code}'
    
    # Открываем страницу приложения
    driver.get(url)
    print(country_code)
    # Ждем 2 секунды, чтобы страница полностью загрузилась
    time.sleep(2)
    
    # Прокручиваем страницу до конца
    driver.execute_script("window.scrollTo(0, document.body.scrollHeight);")
    
    # Ждем 1 секунду после прокрутки
    time.sleep(1)
    
    # Получаем исходный код страницы
    page_source = driver.page_source
    
    # Используем регулярное выражение для поиска цен
    regex_pattern = r'"([^"]*?\sper\sitem)",'
    matches = re.findall(regex_pattern, page_source)
    
    return matches, currency_code


# Функции для работы с WebDriver
def setup_driver():
    chrome_options = Options()
    chrome_options.add_argument("--headless")
    chrome_options.add_argument("--log-level=3")
    service = Service(ChromeDriverManager().install())
    driver = webdriver.Chrome(service=service, options=chrome_options)
    return driver

# Команда start
def start(update: Update, context: CallbackContext) -> None:
    update.message.reply_text('Привет! Отправь мне идентификатор приложения на Google Play, например com.habby.archero')

# Обработка текстовых сообщений
def handle_message(update: Update, context: CallbackContext) -> None:
    app_id = update.message.text
    country_currency_dict = {
    "DZ": "DZD",
    "AU": "AUD",
    "BH": "BHD",
    "BD": "BDT",
    "BO": "BOB",
    "BR": "BRL",
    "KH": "KHR",
    "CA": "CAD",
    "KY": "KYD",
    "CL": "CLP",
    "CO": "COP",
    "CR": "CRC",
    "EG": "EGP",
    "GE": "GEL",
    "GH": "GHS",
    "HK": "HKD",
    "IN": "INR",
    "ID": "IDR",
    "IQ": "IQD",
    "IL": "ILS",
    "JP": "JPY",
    "JO": "JOD",
    "KZ": "KZT",
    "KE": "KES",
    "KR": "KRW",
    "KW": "KWD",
    "MO": "MOP",
    "MY": "MYR",
    "MX": "MXN",
    "MA": "MAD",
    "MM": "MMK",
    "NZ": "NZD",
    "NG": "NGN",
    "OM": "OMR",
    "PK": "PKR",
    "PA": "PAB",
    "PY": "PYG",
    "PE": "PEN",
    "PH": "PHP",
    "QA": "QAR",
    "RU": "RUB",
    "SA": "SAR",
    "RS": "RSD",
    "SG": "SGD",
    "ZA": "ZAR",
    "LK": "LKR",
    "TW": "TWD",
    "TZ": "TZS",
    "TH": "THB",
    "TR": "TRY",
    "UA": "UAH",
    "AE": "AED",
    "US": "USD",
    "VN": "VND",
}


    countries = ["DZ", "AU", "BH", "BD", "BO", "BR", "CA", "KY", "CL", "CO", "CR", 
                 "SV", "GE", "GH", "HK", "IN", "ID", "IQ", "IL", "JP", "JO", "KZ", "KE", "KR", 
                 "KW", "MO", "MY", "MX", "MA", "MM", "NZ", "NG", "OM", "PK", "PY", "PE", "PH", "QA", 
                 "RU", "SA", "RS", "SG", "ZA", "LK", "TW", "TZ", "TH", "TR", "UA", "AE", "US", "VN"]


    driver = setup_driver()
    try:
        # Измененное имя файла для отражения содержания
        with open('prices_by_country_converted.csv', mode='w', newline='', encoding='utf-8') as file:
            writer = csv.writer(file)
            writer.writerow(['Country', 'Currency', 'Original Price Range', 'Converted Price Range (USD)'])

            for country_code in countries:
                prices, currency_code = get_prices_for_country(driver, country_code, country_currency_dict, app_id)
                converted_prices_text = []
                if prices:
                    for price_str in prices:
                        # Извлечение числовых значений из строки цен
                        price_values = re.findall(r'\d[\d,.]*', price_str)
                        for value in price_values:
                            # Удаление разделителей тысяч для корректной конвертации
                            if country_code == 'ID':
                                value = value.replace(",", ".")
                                value = value.replace(".", "")

                            else:    
                                value = value.replace(",", "")
                            # Предполагается, что функция convert_price_to_usd уже реализована
                            converted_price = convert_price_to_usd(value, currency_code)
                            converted_prices_text.append(f"{converted_price} USD")
                else:
                    converted_prices_text = ["No price information found"]

                writer.writerow([country_code, currency_code, '; '.join(prices), '; '.join(converted_prices_text)])

        update.message.reply_text('Обработка завершена, отправляю файл...')
        # Отправка файла
        update.message.reply_document(document=open('prices_by_country_converted.csv', 'rb'))
    finally:
        driver.quit()

def main():
    # Токен вашего бота
    TOKEN = '5541722708:AAGBYrKDTF9DqyBhxI-l5K4azjCKlr-pNEg'

    updater = Updater(TOKEN)

    # Регистрация обработчиков команд
    dispatcher = updater.dispatcher
    dispatcher.add_handler(CommandHandler("start", start))
    dispatcher.add_handler(MessageHandler(Filters.text & ~Filters.command, handle_message))

    # Запуск бота
    updater.start_polling()
    updater.idle()

if __name__ == '__main__':
    main()
