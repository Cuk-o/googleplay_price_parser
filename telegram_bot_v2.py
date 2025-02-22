import nest_asyncio
import time
import re
import csv
import os
import json
import asyncio
import aiohttp
from telegram.ext import (
    Application,
    ApplicationBuilder,
    CommandHandler,
    MessageHandler,
    filters
)
import config

# ----------------- Списки стран и валют -----------------

country_currency_dict = {
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

# Cписок стран, по которым делаем парсинг:
countries = [
    "DZ","EG","AU","BD","BO","BR","CA","CL","CO","CR","GE","GH","HK","IN","ID","IQ",
    "IL","JP","JO","KZ","KE","KR","MO","MY","MX","MA","MM","NZ","NG","PK","PY","PE",
    "PH","QA","RU","SA","RS","SG","ZA","LK","TW","TZ","TH","TR","UA","AE","US","VN"
]

# Курс валюты к USD (примерные/условные значения)
currency_rates = {
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
    "UAH": 41.939132, "AED": 3.671703, "USD": 1,   "VND": 25094.287781
}

# ----------------- Парсинг Google Play -----------------

def create_currency_parsers():
    """
    Готовит парсеры для строк вида 'Rp 10.000', '$9.99', '¥1000' и т.д.
    """
    clean = lambda x: x.replace(' per item', '').replace('\xa0', ' ').strip()
    return {
        'IDR': lambda x: float(clean(x).replace('Rp ', '').replace('.', '').replace(',00', '')),
        'JOD': lambda x: float(clean(x).replace('JOD ', '').replace('.000', '')),
        'TRY': lambda x: float(clean(x).replace('TRY ', '').replace(',', '')),
        'JPY': lambda x: float(clean(x).replace('¥', '').replace(',', '')),
        'KRW': lambda x: float(clean(x).replace('₩', '').replace(',', '')),
        'INR': lambda x: float(clean(x).replace('₹', '').replace(',', '')),
        'VND': lambda x: float(clean(x).replace('₫', '').replace(',', '')),
        'HKD': lambda x: float(clean(x).replace('HK$', '').replace(',', '')),
        'TWD': lambda x: float(clean(x).replace('NT$', '').replace(',', '')),
        'USD': lambda x: float(clean(x).replace('$', '')),
        'AUD': lambda x: float(clean(x).replace('$', '')),
        'NZD': lambda x: float(clean(x).replace('$', '')),
        'CAD': lambda x: float(clean(x).replace('$', '')),
        'SGD': lambda x: float(clean(x).replace('$', '')),
        'ILS': lambda x: float(clean(x).replace('₪', '').replace(',', '')),
        'ZAR': lambda x: float(clean(x).replace('R ', '').replace(' ', '').replace(',', '.')),
        # По умолчанию пытаемся найти первое совпадение с числами:
        'DEFAULT': lambda x: float(re.search(r'[\d,.]+', clean(x)).group(0).replace(',', ''))
    }

async def convert_price_to_usd_google(price_str, currency_code):
    """
    Преобразует диапазон цен типа '¥100 - ¥200 per item' в (min_usd, max_usd).
    Если это одна цена, min=max.
    """
    try:
        first_range = price_str.split(';')[0].strip()  # иногда там "x; y" -> берем первую
        if '-' in first_range:
            min_price_str, max_price_str = [p.strip() for p in first_range.split('-')]
        else:
            min_price_str = max_price_str = first_range

        parsers = create_currency_parsers()
        parser = parsers.get(currency_code, parsers['DEFAULT'])

        min_price = parser(min_price_str)
        max_price = parser(max_price_str)

        rate = currency_rates.get(currency_code, 1)
        min_usd = max(round(min_price / rate, 2), 0.01)
        max_usd = max(round(max_price / rate, 2), 0.01)

        return (min_usd, max_usd)
    except Exception as e:
        print(f"Error parsing {currency_code}: {price_str}")
        print(f"Error: {e}")
        return (0.0, 0.0)

async def get_prices_for_country_google(country_code, app_id):
    """
    Вызывается для каждой страны.
    Загружает страницу Google Play и ищет текст: "XXX per item"
    Возвращает (список_найденных_цен, currency_code, статус).
    """
    try:
        currency_code = country_currency_dict.get(country_code, "USD")
        url = f'https://play.google.com/store/apps/details?id={app_id}&hl=en&gl={country_code}'

        async with aiohttp.ClientSession() as session:
            async with session.get(url) as response:
                page_content = await response.text()
                print(f"[Google] {country_code}")

                if "In-app purchases" not in page_content:
                    print("На странице нет текста 'In-app purchases'")
                    return None, currency_code, 'noinapp'
                if "We're sorry, the requested URL was not found on this server." in page_content:
                    print("404")
                    return None, currency_code, '404'

                # Ищем шаблон, например, "XXX per item"
                matches = re.findall(r'"([^"]*?\sper\sitem)",', page_content)
                return matches, currency_code, True

    except Exception as e:
        print(f"Error for {country_code}: {e}")
        return None, None, False

async def fetch_prices_google(update, context, app_id):
    """
    Циклично обходит список стран, собирает In-App Purchases и сохраняет в CSV.
    """
    await update.message.reply_text('Обработка для Google Play началась...')
    collected_data = []
    batch_size = 5  # разом обрабатываем по 5 стран (можно менять)

    for i in range(0, len(countries), batch_size):
        batch = countries[i:i+batch_size]
        tasks = [get_prices_for_country_google(cc, app_id) for cc in batch]
        batch_results = await asyncio.gather(*tasks)

        for j, result in enumerate(batch_results):
            prices, currency_code, success = result
            country_code = batch[j]

            if success is True and prices:
                # Берём первую найденную цену/диапазон
                min_price_usd, max_price_usd = await convert_price_to_usd_google(prices[0], currency_code)
                collected_data.append([
                    min_price_usd,
                    max_price_usd,
                    country_code,
                    currency_code,
                    prices[0]
                ])
            elif success == '404':
                print(f"{country_code}: Страница не найдена (404).")
            else:
                # либо 'noinapp', либо ошибка
                print(f"{country_code}: Данные не найдены.")

    # Сортируем по Min Price
    sorted_data = sorted(collected_data, key=lambda x: float(x[0]))
    filepath = os.path.join(config.CONST_PATH, f"{app_id}_google.csv")

    with open(filepath, mode='w', newline='', encoding='utf-8') as file:
        writer = csv.writer(file)
        writer.writerow(['Min Price (USD)', 'Max Price (USD)', 'Country', 'Currency', 'Original Price Range'])
        writer.writerows(sorted_data)

    return filepath

# ----------------- Парсинг App Store через JSON (Sensor Tower API) -----------------

def extract_numeric_price(price_str: str):
    """
    Извлекает число из строки вида '￦29,000' -> 29000, '$19.99' -> 19.99, etc.
    Убираем все не-цифры (кроме . и ,), затем удаляем запятые.
    """
    clean_str = re.sub(r'[^\d.,]+', '', price_str)  # оставим только цифры, точки, запятые
    clean_str = clean_str.replace(',', '')          # убираем запятые
    if not clean_str:
        return 0.0
    try:
        return float(clean_str)
    except:
        return 0.0

# Словарь для перевода арабских цифр в обычные
arabic_digits_map = {
    '٠': '0', '١': '1', '٢': '2', '٣': '3',
    '٤': '4', '٥': '5', '٦': '6', '٧': '7',
    '٨': '8', '٩': '9'
}

# Словарь конфигураций для каждой валюты
currency_configs = {
    "DZD": {
        "strip_strings": ["‏US", "US$"],  # Удаляем оба варианта префикса
        "arabic_digits_map": None,
        "arabic_decimal_dot": None,
        "thousands_sep": None,
        "decimal_sep": ",",        # Используем запятую как десятичный разделитель
        "is_already_usd": True     # Важно! Указываем что это уже в USD
    },

    "BRL": {
        "strip_strings": ["R$"],
        "arabic_digits_map": None,
        "arabic_decimal_dot": None,
        "thousands_sep": ".",
        "decimal_sep": ",",
        "is_already_usd": False
    },

    # --- Египет (арабские цифры) ---
    "EGP": {
        "strip_strings": ["ج.م.‏"],  # что вырезать
        "arabic_digits_map": arabic_digits_map,
        "arabic_decimal_dot": "٫",   # заменить на "."
        "thousands_sep": None,       # нет явного (или игнорируем)
        "decimal_sep": None,         # допустим, нет
        "is_already_usd": False
    },
    # --- Колумбия ---
    "COP": {
        "strip_strings": [],         # ничего особого
        "arabic_digits_map": None,
        "arabic_decimal_dot": None,
        "thousands_sep": ".",        # точка - разделитель тысяч
        "decimal_sep": ",",          # запятая - дробная
        "is_already_usd": False
    },
    # --- Чили ---
    "CLP": {
        "strip_strings": [],
        "arabic_digits_map": None,
        "arabic_decimal_dot": None,
        "thousands_sep": ".",
        "decimal_sep": ",",
        "is_already_usd": False
    },
    # --- США (цены в USD) ---
    "USD": {
        "strip_strings": [],
        "arabic_digits_map": None,
        "arabic_decimal_dot": None,
        "thousands_sep": None,
        "decimal_sep": None,
        "is_already_usd": True
    },
    # Можно добавить BRL, EUR, INR и пр. по аналогии.
    # --- Значение по умолчанию ---
    "DEFAULT": {
        "strip_strings": [],
        "arabic_digits_map": None,
        "arabic_decimal_dot": None,
        "thousands_sep": None,
        "decimal_sep": None,
        "is_already_usd": False
    }
}

async def convert_price_to_usd_apple(price_str: str, currency_code: str):
    """
    Универсальный парсер, использующий для каждой валюты (CLP, COP, EGP и т.д.)
    собственные настройки. Возвращает (price_usd, price_usd).
    """

    try:
        # 1) Если в самой строке встречается "USD", считаем 1:1
        if 'USD' in price_str.upper() or 'DZD' in price_str.upper():
            numeric_part = re.sub(r'[^0-9.,]+', '', price_str)
            numeric_part = numeric_part.replace(',', '')
            price_usd = float(numeric_part) if numeric_part else 0.0
            return (price_usd, price_usd)

        # 2) Берем конфиг для конкретной валюты, либо DEFAULT
        config = currency_configs.get(currency_code, currency_configs["DEFAULT"])

        # 2.1) Вырезаем указанные подстроки (например, "ج.م.‏")
        for s in config["strip_strings"]:
            price_str = price_str.replace(s, "")

        # 2.2) Если нужно заменить арабские цифры
        if config["arabic_digits_map"]:
            # Если есть arabic_decimal_dot, меняем на "."
            if config["arabic_decimal_dot"]:
                price_str = price_str.replace(config["arabic_decimal_dot"], ".")
            # Переводим цифры
            converted = []
            for ch in price_str:
                if ch in config["arabic_digits_map"]:
                    converted.append(config["arabic_digits_map"][ch])
                else:
                    converted.append(ch)
            price_str = ''.join(converted)

        # 2.3) Удаляем всё, кроме цифр, точек и запятых
        clean_str = re.sub(r'[^0-9.,]+', '', price_str)

        # 2.4) Удаляем разделитель тысяч
        if config["thousands_sep"]:
            clean_str = clean_str.replace(config["thousands_sep"], '')

        # 2.5) Заменяем разделитель дробей на точку (например, ',' -> '.')
        if config["decimal_sep"] and config["decimal_sep"] != '.':
            clean_str = clean_str.replace(config["decimal_sep"], '.')

        # 3) Превращаем результат в число
        numeric_price = float(clean_str) if clean_str else 0.0

        # 4) Если в конфиге указано is_already_usd=True, ничего не делим
        if config["is_already_usd"]:
            price_usd = numeric_price
        else:
            # Ищем курс, если нет — считаем 1.0
            rate = currency_rates.get(currency_code, 1.0)
            price_usd = numeric_price / rate

        # 5) Округляем и возвращаем
        price_usd = max(round(price_usd, 2), 0.01)
        return (price_usd, price_usd)

    except Exception as e:
        print(f"[Apple] Ошибка парсинга '{currency_code}': '{price_str}'")
        print(e)
        return (0.0, 0.0)

async def get_prices_for_country_apple(country_code, apple_id):
    """
    Запрашивает JSON-данные: https://app.sensortower.com/api/ios/apps/<apple_id>?country=<country_code>
    Ищет блок top_in_app_purchases -> [ {iap...}, ... ].
    Возвращает список словарей с полями:
    {
      "name": <название iap>,
      "price_str": <ориг. цена, напр. "￦29,000">,
      "currency_code": <код валюты>,
      "duration": <строка, напр. "P1M">,
      "min_price_usd": <число>,
      "max_price_usd": <число>
    }
    Или None, если данных нет.
    """
    url = f"https://app.sensortower.com/api/ios/apps/{apple_id}?country={country_code}"
    currency_code = country_currency_dict.get(country_code, "USD")

    async with aiohttp.ClientSession() as session:
        try:
            async with session.get(url) as response:
                if response.status == 404:
                    print(f"[Apple] {country_code}: 404 for {url}")
                    return None

                text = await response.text()

                # Парсим JSON
                data = json.loads(text)

                if "top_in_app_purchases" not in data:
                    print(f"[Apple] {country_code}: Нет top_in_app_purchases")
                    return None

                # top_in_app_purchases: {"KR": [ {iap...}, ...], "US": [...], ...}
                iaps_for_country = data["top_in_app_purchases"].get(country_code)
                if not iaps_for_country:
                    print(f"[Apple] {country_code}: Нет IAP для страны.")
                    return None

                results = []
                for iap in iaps_for_country:
                    price_str = iap.get("price", "")
                    name = iap.get("name", "")
                    duration = iap.get("duration", "")

                    min_price_usd, max_price_usd = await convert_price_to_usd_apple(price_str, currency_code)

                    results.append({
                        "name": name,
                        "price_str": price_str,
                        "currency_code": currency_code,
                        "duration": duration,
                        "min_price_usd": min_price_usd,
                        "max_price_usd": max_price_usd
                    })
                return results

        except Exception as e:
            print(f"[Apple] {country_code} Error: {e}")
            return None


async def fetch_prices_apple(update, context, apple_id):
    """
    Обходит только EGP (Египет), собирает IAP из JSON Sensor Tower, пишет в CSV.
    """
    await update.message.reply_text("Обработка для App Store (JSON API) началась...")
    collected_data = []
    
    # Проверяем только для EGP (Египет)
    country_code = "EG"
    
    try:
        iaps_list = await get_prices_for_country_apple(country_code, apple_id)
        
        if iaps_list:
            for iap in iaps_list:
                collected_data.append([
                    iap["min_price_usd"],
                    iap["max_price_usd"],
                    country_code,
                    iap["currency_code"],
                    iap["price_str"],
                    iap["name"],
                    iap["duration"],
                ])
            print(f"[Apple] {country_code}: Найдены данные.")
        else:
            print(f"[Apple] {country_code}: Данные не найдены или пусты.")
    
    except Exception as e:
        print(f"[Apple] {country_code} Error: {e}")

    # Сортируем по min_price_usd
    sorted_data = sorted(collected_data, key=lambda x: float(x[0]))

    filepath = os.path.join(config.CONST_PATH, f"{apple_id}_apple_EGP.csv")
    with open(filepath, mode='w', newline='', encoding='utf-8') as file:
        writer = csv.writer(file)
        writer.writerow([
            'Min Price (USD)',
            'Max Price (USD)',
            'Country',
            'Currency',
            'Original Price',
            'IAP Name',
            'Duration'
        ])
        writer.writerows(sorted_data)

    return filepath

# ----------------- Телеграм-бот -----------------

async def start(update, context):
    await context.bot.send_message(
        chat_id=update.effective_chat.id,
        text=(
            "Привет! Отправьте ссылку на приложение из Google Play "
            "(формата https://play.google.com/store/apps/details?id=xxx) "
            "или из App Store (формата https://apps.apple.com/xx/app/yyy/idNNNN)."
        )
    )

async def handle_message(update, context):
    start_time = time.time()
    text = update.message.text

    user = update.effective_user
    username = user.username if user.username else "неизвестный"
    full_name = f"{user.first_name} {user.last_name if user.last_name else ''}".strip()

    # Определяем, Google Play или App Store
    if "play.google.com" in text:
        # Ищем ?id=com.example
        match = re.search(r'id=([\w\d\.]+)', text)
        if not match:
            await context.bot.send_message(
                chat_id=update.effective_chat.id,
                text="Не удалось найти идентификатор приложения в Google Play ссылке."
            )
            return
        app_id = match.group(1)
        filepath = await fetch_prices_google(update, context, app_id)

        if filepath and os.path.exists(filepath):
            with open(filepath, 'rb') as file:
                await context.bot.send_document(chat_id=update.effective_chat.id, document=file)
        else:
            await context.bot.send_message(
                chat_id=update.effective_chat.id,
                text=f"Информация о ценах для приложения {app_id} не найдена или страница недоступна."
            )

    elif "apps.apple.com" in text:
        # Ищем /idNNN
        match = re.search(r'/id(\d+)', text)
        if not match:
            await context.bot.send_message(
                chat_id=update.effective_chat.id,
                text="Не удалось найти идентификатор приложения (idNNN) в App Store ссылке."
            )
            return
        apple_id = match.group(1)
        filepath = await fetch_prices_apple(update, context, apple_id)

        if filepath and os.path.exists(filepath):
            with open(filepath, 'rb') as file:
                await context.bot.send_document(chat_id=update.effective_chat.id, document=file)
        else:
            await context.bot.send_message(
                chat_id=update.effective_chat.id,
                text=f"Информация о ценах для приложения {apple_id} не найдена или страница недоступна."
            )

    else:
        # Ни Google, ни Apple
        await context.bot.send_message(
            chat_id=update.effective_chat.id,
            text="Ссылка не распознана. Отправьте ссылку на приложение Google Play или App Store."
        )

    # Логирование
    end_time = time.time()
    total_time = end_time - start_time
    print(f"Время выполнения: {total_time:.2f} секунд.")
    print(f"Запрос от {full_name} (@{username})")

    # Сохраняем в лог
    logs_path = os.path.join(config.CONST_PATH, "logs.log")
    with open(logs_path, mode='a', newline='', encoding='utf-8') as f:
        writer = csv.writer(f)
        current_time = time.strftime("%Y-%m-%d %H:%M:%S", time.gmtime())
        writer.writerow([
            current_time,
            f"Время: {total_time:.2f} сек, пользователь: {full_name} (@{username})"
        ])

async def main():
    application = ApplicationBuilder().token(config.CONST_TOKEN).build()
    application.add_handler(CommandHandler("start", start))
    application.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, handle_message))
    await application.run_polling()

if __name__ == '__main__':
    nest_asyncio.apply()
    asyncio.run(main())
