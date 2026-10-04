#!/bin/bash
API="http://localhost:8080/api"

echo "1. Массовое создание 10 игроков (проверка 7: Конвейеризация)..."
curl -s -X POST "$API/players/batch" \
  -H "Content-Type: application/json" \
  -d '{
    "players": [
      {"id": "1", "name": "P1", "level": 1, "region": "EU"},
      {"id": "2", "name": "P2", "level": 2, "region": "EU"},
      {"id": "3", "name": "P3", "level": 3, "region": "US"},
      {"id": "4", "name": "P4", "level": 4, "region": "US"},
      {"id": "5", "name": "P5", "level": 5, "region": "ASIA"},
      {"id": "6", "name": "P6", "level": 6, "region": "ASIA"},
      {"id": "7", "name": "P7", "level": 7, "region": "EU"},
      {"id": "8", "name": "P8", "level": 8, "region": "RU"},
      {"id": "9", "name": "P9", "level": 9, "region": "RU"},
      {"id": "10", "name": "P10", "level": 10, "region": "RU"}
    ]
  }'
echo ""

echo "2. Создание игрока 1001 (проверка 3: Hash)..."
curl -s -X POST "$API/players/1001" \
  -H "Content-Type: application/json" \
  -d '{"name": "Champion", "level": 10, "region": "RU"}'
echo ""

echo "3. Чтение игрока 1001 для прогрева кэша (проверка 4: Кэширование)..."
curl -s -X GET "$API/players/1001"
echo ""

echo "4. Фиксация входа игрока 1001 (проверка 5: Счетчик входов)..."
curl -s -X POST "$API/players/1001/login"
echo ""

echo "5. Добавление очков в лидерборд (проверка 3: Лидерборд)..."
curl -s -X POST "$API/leaderboard/score" \
  -H "Content-Type: application/json" \
  -d '{"player_id": "1001", "score": 2500}'
echo ""

echo "6. Добавление достижения (проверка 3: Достижения)..."
curl -s -X POST "$API/players/1001/achievements" \
  -H "Content-Type: application/json" \
  -d '{"name": "First Blood"}'
echo ""

echo "7. Обновление уровня -> отправка уведомления в стрим (проверка 6: Streams)..."
curl -s -X PATCH "$API/players/1001/level" \
  -H "Content-Type: application/json" \
  -d '{"delta": 1}'
echo ""

echo "8. Повторный GET для восстановления кэша после PATCH..."
curl -s -X GET "$API/players/1001"
echo ""

echo "Данные успешно подготовлены!"