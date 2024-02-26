import nest_asyncio
import time
import re
import csv
import os
from telegram.ext import Application, CommandHandler, MessageHandler, filters
import aiohttp
import asyncio
import config

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

async def convert_price_to_usd(price_str, currency_code):
    price_match = re.search(r"(\d[\d,.]*)", price_str.replace(",", ""))
    if price_match:
        price = float(price_match.group(1).replace(",", ""))
        rate = currency_rates.get(currency_code.upper(), 1)
        if currency_code == 'IDR':
            return round(price / rate * 1000, 2)
        else:
            return round(price / rate, 2)
    return 0

async def fetch_page(url):
    async with aiohttp.ClientSession() as session:
        async with session.get(url) as response:
            return await response.text()

async def get_prices_for_country(country_code, app_id):
    currency_code = country_currency_dict.get(country_code, "USD")
    url = f'https://play.google.com/store/apps/details?id={app_id}&hl=en&gl={country_code}'
    page_content = await fetch_page(url)
    print(country_code)
    # Проверка наличия текста "In-app purchases"
    if country_code == 'DZ':
        if "In-app purchases" not in page_content:
            print("На странице нет текста 'In-app purchases'")
            return None, currency_code, 'noinapp'
        if "We're sorry, the requested URL was not found on this server." in page_content:
            print("404")
            return None, currency_code, '404'

    # Извлечение информации о ценах с помощью регулярных выражений
    matches = re.findall(r'"([^"]*?\sper\sitem)",', page_content)
    return matches, currency_code, True

async def fetch_prices(update, context, app_id):
    tasks = [get_prices_for_country(country_code, app_id) for country_code in countries]
    results = await asyncio.gather(*tasks)

    await update.message.reply_text('Обработка началась.')

    collected_data = []
    processed_countries = 0
    
    for result in results:
        prices, currency_code, success = result
        processed_countries += 1
        country_code = countries[processed_countries - 1]  # Получаем код страны из списка
        if success and prices:
            converted_prices_text = [str(await convert_price_to_usd(price, currency_code)) for price in prices]
            collected_data.append([country_code, currency_code, '; '.join(prices), '; '.join([f"{price} USD" for price in converted_prices_text])])
        elif success in ["noinapp", "404"]:
            print(f"{country_code}: Нет данных о покупках в приложении или страница не найдена.")
        else:
            print(f"{country_code}: Данные не найдены.")
            continue

#        converted_prices_text = [await convert_price_to_usd(price, currency_code) for price in prices]
 #       collected_data.append([country_code, currency_code, '; '.join(prices), '; '.join([f"{price} USD" for price in converted_prices_text])])

    # Сортировка списка по конвертированной цене в USD (4-й элемент списка, индекс 3)
    sorted_data = sorted(collected_data, key=lambda x: x[3])

    # Сохранение отсортированных данных в файл CSV
    filepath = os.path.join(config.CONST_PATH, f"{app_id}.csv")
    with open(filepath, mode='w', newline='', encoding='utf-8') as file:
        writer = csv.writer(file)
        writer.writerow(['Country', 'Currency', 'Original Price Range', 'Converted Price Range (USD)', 'Currency Label'])
        for item in sorted_data:
            writer.writerow(item)
    
    return filepath

async def start(update, context):
    await context.bot.send_message(chat_id=update.effective_chat.id, text="Привет! Поделись ссылкой из Google Play для определения идентификатора приложения.")

async def handle_message(update, context):
    start_time = time.time()  # Запоминаем время начала обработки
    text = update.message.text
    match = re.search(r'id=(\w+\.[\w\d_\.]+)', text)
    
    if match:
        app_id = match.group(1)
        # Формируем предполагаемый путь к файлу
        filepath = os.path.join(config.CONST_PATH, f"{app_id}.csv")
        
        # Проверяем, существует ли файл
        if os.path.exists(filepath):
            # Если файл существует, отправляем его без повторной проверки цен
            print("Отправка существующего файла.")
            with open(filepath, 'rb') as file:
                await context.bot.send_document(chat_id=update.effective_chat.id, document=file)
        else:
            # Если файла нет, запускаем процесс сбора данных и создания файла
            filepath = await fetch_prices(update, context, app_id)
            
            if filepath is not None:
                with open(filepath, 'rb') as file:
                    await context.bot.send_document(chat_id=update.effective_chat.id, document=file)
            else:
                await context.bot.send_message(chat_id=update.effective_chat.id, text=f'Информация о ценах для приложения {app_id} не найдена или страница приложения отсутствует.')
        
        end_time = time.time()  # Запоминаем время окончания обработки
        total_time = end_time - start_time  # Вычисляем общее время выполнения
        print(f"Время выполнения: {total_time:.2f} секунд.")
    else:
        await context.bot.send_message(chat_id=update.effective_chat.id, text="Не удалось найти идентификатор приложения. Пожалуйста, отправьте корректный URL.")

async def main():
    application = Application.builder().token(config.CONST_TOKEN).build()

    application.add_handler(CommandHandler("start", start))
    application.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, handle_message))

    await application.run_polling()

if __name__ == '__main__':
    nest_asyncio.apply()
    asyncio.run(main())
