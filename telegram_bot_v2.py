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

countries = ["DZ", "EG", "AU", "BD", "BO", "BR", "CA", "CL", "CO", "CR", 
            "GE", "GH", "HK", "IN", "ID", "IQ", "IL", "JP", "JO", "KZ", "KE", "KR", 
            "MO", "MY", "MX", "MA", "MM", "NZ", "NG", "PK", "PY", "PE", "PH", "QA", 
            "RU", "SA", "RS", "SG", "ZA", "LK", "TW", "TZ", "TH", "TR", "UA", "AE", "US", "VN"]

currency_rates = {
    "DZD": 134.966, "AUD": 1.583982, "BHD": 0.376241, "BDT": 109.73,
    "BOB": 6.909550, "BRL": 5.906974, "CAD": 1.433827, "KYD": 0.833,
    "CLP": 986.794638, "COP": 4227.599492, "CRC": 504.817577, "EGP": 50.291311,
    "GEL": 2.867107, "GHS": 15.187930, "HKD": 7.787505, "INR": 86.249922,
    "IDR": 16149.393463, "IQD": 1309.703222, "ILS": 3.587793, "JPY": 155.855438,
    "JOD": 0.709118, "KZT": 519.503277, "KES": 129.264801, "KRW": 1432.185253,
    "KWD": 0.308060, "MOP": 8.021963, "MYR": 4.392292, "MXN": 20.245294,
    "MAD": 10.007902, "MMK": 2099.980901, "NZD": 1.752597, "NGN": 1550.620034,
    "OMR": 0.384454, "PKR": 278.655722, "PYG": 7918.619687, "PEN": 3.712514,
    "PHP": 58.388686, "QAR": 3.639992, "RUB": 97.929483, "SAR": 3.750482,
    "RSD": 112.125584, "SGD": 1.348339, "ZAR": 18.384263, "LKR": 298.761937,
    "TWD": 32.687009, "TZS": 2507.601986, "THB": 33.712166, "TRY": 35.678472,
    "UAH": 41.939132, "AED": 3.671703, "USD": 1, "VND": 25094.287781
}


async def convert_price_to_usd(price_str, currency_code):
    price_parts = price_str.split('-')
    converted_prices = []

    for part in price_parts:
        # Обработка каждой части диапазона цен
        price_match = re.search(r"(\d[\d,.]*)", part)
        if price_match:
            price_str = price_match.group(1)

            # Удаление разделителей тысяч и десятичных знаков
            last_dot_position = max(price_str.rfind('.'), price_str.rfind(','))
            if last_dot_position != -1 and len(price_str) - last_dot_position - 1 == 2:
                price_str = price_str[:last_dot_position]
            price_str = price_str.replace(",", "").replace(".", "")

            # Преобразование в число
            price = float(price_str)
            if currency_code == 'JOD':
                price = price / 1000

            # Получение курса валюты и конвертация
            rate = currency_rates.get(currency_code.upper(), 1)
            converted_price = round(price / rate, 2)
            converted_prices.append(converted_price)
        else:
            # Если не удалось преобразовать, добавляем 0
            converted_prices.append(0)

    if len(converted_prices) == 2:
        # Возвращаем минимальную и максимальную цены, если есть диапазон
        return converted_prices[0], converted_prices[1]
    elif len(converted_prices) == 1:
        # Возвращаем одну цену как минимальную и максимальную, если диапазона нет
        return converted_prices[0], converted_prices[0]
    else:
        # В случае отсутствия цен, возвращаем 0, 0
        return 0, 0


async def fetch_page(url):
    async with aiohttp.ClientSession() as session:
        async with session.get(url) as response:
            return await response.text()

async def get_prices_for_country(country_code, app_id):
    try:
        currency_code = country_currency_dict.get(country_code, "USD")
        url = f'https://play.google.com/store/apps/details?id={app_id}&hl=en&gl={country_code}'
        
        async with aiohttp.ClientSession() as session:
            async with session.get(url) as response:
                page_content = await response.text()
                print(country_code)
                
                if "In-app purchases" not in page_content:
                    print("На странице нет текста 'In-app purchases'")
                    return None, currency_code, 'noinapp'
                if "We're sorry, the requested URL was not found on this server." in page_content:
                    print("404")
                    return None, currency_code, '404'
                
                matches = re.findall(r'"([^"]*?\sper\sitem)",', page_content)
                del page_content  # Clear memory
                return matches, currency_code, True
                
    except Exception as e:
        print(f"Error for {country_code}: {e}")
        return None, currency_code, False
    

async def fetch_prices(update, context, app_id):
    await update.message.reply_text('Обработка началась.')
    collected_data = []
    batch_size = 5
    
    for i in range(0, len(countries), batch_size):
        batch = countries[i:i+batch_size]
        tasks = [get_prices_for_country(country_code, app_id) for country_code in batch]
        batch_results = await asyncio.gather(*tasks)
        
        for j, result in enumerate(batch_results):
            prices, currency_code, success = result
            country_code = batch[j]
            
            if success and prices:
                converted_prices = [await convert_price_to_usd(price, currency_code) for price in prices]
                min_converted_price = min(min_price for min_price, _ in converted_prices)
                max_converted_price = max(max_price for _, max_price in converted_prices)
                collected_data.append([country_code, currency_code, '; '.join(prices), 
                                    str(min_converted_price), str(max_converted_price)])
            elif success == "404":
                print(f"{country_code}: Нет данных о покупках в приложении или страница не найдена.")
                return
            else:
                print(f"{country_code}: Данные не найдены.")

    sorted_data = sorted(collected_data, key=lambda x: float(x[4]))
    filepath = os.path.join(config.CONST_PATH, f"{app_id}.csv")
    
    with open(filepath, mode='w', newline='', encoding='utf-8') as file:
        writer = csv.writer(file)
        writer.writerow(['Country', 'Currency', 'Original Price Range', 
                        'Converted Price Min (USD)', 'Converted Price Max (USD)'])
        writer.writerows(sorted_data)
    
    return filepath



async def start(update, context):
    await context.bot.send_message(chat_id=update.effective_chat.id, text="Привет! Поделись ссылкой из Google Play для определения идентификатора приложения.")

async def handle_message(update, context):
    start_time = time.time()  # Запоминаем время начала обработки
    text = update.message.text
    match = re.search(r'id=(\w+\.[\w\d_\.]+)', text)
    user = update.effective_user
    username = user.username if user.username else "неизвестный"  # Имя пользователя в Telegram (логин)
    full_name = f"{user.first_name} {user.last_name if user.last_name else ''}".strip()

    if match:
        app_id = match.group(1)
        # Формируем предполагаемый путь к файлу
        filepath = os.path.join(config.CONST_PATH, f"{app_id}.csv")
        
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
        print(f"Запрос на получение цен для приложения {app_id} инициирован пользователем {full_name} (@{username})")
            # Сохранение логов
        filepath = os.path.join(config.CONST_PATH, "logs.log")

        # Открытие файла в режиме добавления 'a'
        with open(filepath, mode='a', newline='', encoding='utf-8') as file:
            writer = csv.writer(file)
            current_time = time.strftime("%Y-%m-%d %H:%M:%S", time.gmtime())  # Получаем текущее время для лога
            # Оборачиваем сообщение лога в список, чтобы записать его как одну строку в CSV
            writer.writerow([current_time, f"Время выполнения: {total_time:.2f} секунд. Запрос на получение цен для приложения {app_id} инициирован пользователем {full_name} (@{username})"])    

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

