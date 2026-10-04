# Домашняя работа №1. Redis — GameHub

Бэкенд платформы «GameHub» на Go (`echo` + `go-redis/v9`): управление профилями
игроков, турнирным рейтингом, достижениями и уведомлениями. Redis выступает
единым хранилищем и кешем. Развёрнута отказоустойчивая конфигурация
**Master + 2 Replica + 3 Sentinel** с персистентностью и защитой от split-brain.

---

## Архитектура

```
                  ┌───────────────┐
   REST (8080) ──►│   Application  │ (echo + go-redis FailoverClient)
                  └──────┬────────┘
                         │ подключается через Sentinel (mymaster)
              ┌──────────┼──────────┐
              ▼          ▼          ▼
         Sentinel-1  Sentinel-2  Sentinel-3
         (26379)     (26380)     (26381)      quorum = 2
              └──────────┼──────────┘
                         │ автообнаружение мастера
              ┌──────────┴──────────┐
              ▼                     ▼
        redis-master (6379)    replica-1 (6380), replica-2 (6381)
        RDB + AOF              replicaof master 6379, read-only
```

- **Application** — REST API, взаимодействует с Redis только через Sentinel
  (`redis.NewFailoverClient`), проверяет соединение при старте.
- **Redis Master** — записи, персистентность RDB + AOF, политика вытеснения,
  защита от split-brain.
- **Redis Replicas (2 шт.)** — обслуживают чтение, read-only.
- **Redis Sentinel (3 шт.)** — обнаружение отказа и автофейловер мастера.

---

## Структура данных в Redis

| Данные | Тип Redis | Ключ / значение | TTL | Описание |
|---|---|---|---|---|
| Профиль игрока | Hash | `player:{id}` → `name`, `level`, `region`, `created_at` | ∞ | Основные данные игрока |
| Счётчик входов | String | `logins:{id}` → числовое значение | 24 ч | INCR при каждом входе (атомарно, Lua) |
| Кеш профиля | String | `cache:player:{id}` → JSON профиля | 60 с | Быстрый доступ |
| Лидерборд | Sorted Set | `tournament:main` → member=`player_id`, score=очки | ∞ | Рейтинг игроков |
| Достижения | Set | `achievements:{id}` → названия достижений | ∞ | Уникальные достижения игрока |
| Очередь уведомлений | Stream | `notifications` → `player_id`, `type`, `message`, `timestamp` | 7 дней | Поток уведомлений |

Группа потребителей стрима: `notifications-group` (создаётся через
`XGROUP CREATE notifications notifications-group $ MKSTREAM`). Фоновый consumer
читает через `XREADGROUP`, выводит в консоль и подтверждает через `XACK`.
Хранение 7 дней обеспечивается `XADD ... MINID ~` (приближённая политика удаления).

---

## Запуск

```bash
# 1. Собрать и поднять весь стек (мастер + 2 реплики + 3 Sentinel + приложение)
docker compose up -d --build

# 2. Убедиться, что все контейнеры в статусе running
docker compose ps

# 3. Проверить репликацию (ожидается connected_slaves:2)
docker compose exec redis-master redis-cli INFO replication

# 4. Проверить, что Sentinel видит мастера
docker compose exec sentinel-1 redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster

# 5. Логи приложения (consumer уведомлений, ошибки подключения)
docker compose logs -f app
```

API доступно на `http://localhost:8080/api`.

---

## Инфраструктура (Docker Compose)

| Сервис | Хост-порт | Настройки |
|---|---|---|
| `redis-master` | `6379` | `appendonly yes`, `save 900 1 300 10 60 10000`, `maxmemory 256mb`, `maxmemory-policy volatile-lru`, `min-replicas-to-write 1`, `min-replicas-max-lag 10` |
| `replica-1` | `6380` | `--replicaof master 6379`, `--replica-read-only yes`, `--appendonly yes` |
| `replica-2` | `6381` | `--replicaof master 6379`, `--replica-read-only yes`, `--appendonly yes` |
| `sentinel-1/2/3` | `26379/26380/26381` | `sentinel monitor mymaster master 6379 2`, `down-after-milliseconds 5000`, `failover-timeout 60000`, `resolve-hostnames yes`, `announce-hostnames yes` |
| `app` | `8080` | Go-приложение (`FailoverClient`, `MasterName: mymaster`) |

Все контейнеры находятся в общей сети `redis-net`. Конфиги Sentinel лежат в
`sentinel1.conf`, `sentinel2.conf`, `sentinel3.conf`.

---

## API

Базовый путь: `http://localhost:8080/api`

| Метод | Путь | Что делает | Операции в Redis |
|---|---|---|---|
| POST | `/api/players/{id}` | Создать/обновить профиль | `HSET player:{id} name level region created_at` |
| GET | `/api/players/{id}` | Получить профиль (с кешем) | кеш `GET cache:player:{id}`, при промахе `HGETALL player:{id}` + `SET ... EX 60` |
| PATCH | `/api/players/{id}/level` | Обновить уровень | `HINCRBY player:{id} level {delta}` + `DEL cache` + `XADD notifications` |
| POST | `/api/players/{id}/login` | Зафиксировать вход | Lua: `INCR logins:{id}` + `EXPIRE logins:{id} 86400` |
| POST | `/api/leaderboard/score` | Добавить/обновить очки | `ZINCRBY tournament:main {score} {player_id}` |
| GET | `/api/leaderboard/top?limit=10` | Топ-10 игроков | `ZREVRANGE tournament:main 0 9 WITHSCORES` |
| GET | `/api/leaderboard/rank/{playerId}` | Место игрока | `ZRANK tournament:main {player_id}` |
| POST | `/api/players/{id}/achievements` | Добавить достижение | `SADD achievements:{id} {name}` |
| GET | `/api/players/{id}/achievements/{name}` | Проверить наличие | `SISMEMBER achievements:{id} {name}` |
| GET | `/api/players/{id1}/achievements/common/{id2}` | Общие достижения | `SINTER achievements:{id1} achievements:{id2}` |
| POST | `/api/players/batch` | Массовое создание профилей | конвейер: пачка `HSET` через `Pipeline()` |

---

## Демонстрация через curl

Подготовка демо-данных одной командой:

```bash
./seed.sh
```

Проверка Redis напрямую:

```bash
# Репликация
docker compose exec redis-master redis-cli INFO replication

# Кеш и его TTL
docker compose exec redis-master redis-cli TTL cache:player:1001

# Очередь уведомлений и группа
docker compose exec redis-master redis-cli XLEN notifications
docker compose exec redis-master redis-cli XINFO GROUPS notifications

# Счётчик входов
docker compose exec redis-master redis-cli GET logins:1001
```

---

## Демонстрация отказоустойчивости

**Sentinel-фейловер:** остановить мастер — Sentinel переключит трафик на реплику:

```bash
docker compose stop redis-master
sleep 15
docker compose exec sentinel-1 redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster
curl -s http://localhost:8080/api/players/1001   # приложение продолжает работать
docker compose start redis-master
```

**Защита от split-brain:** `min-replicas-to-write 1` — если приостановить все
реплики, мастер начинает отклонять записи:

```bash
docker compose pause replica-1 replica-2
# Запись должна вернуть ошибку (min-replicas-to-write)
curl -s -X POST "$API/players/1001/login"
docker compose unpause replica-1 replica-2
```

---

## Самопроверка

```bash
docker compose up -d --build
./seed.sh
python3 dz1_check.py
```

Скрипт `dz1_check.py` проверяет инфраструктуру, репликацию и Sentinel, структуры
данных, кеш, счётчик входов, стрим, конвейеризацию и защиту от split-brain.

---

## Структура репозитория

```
.
├── cmd/main.go            # точка входа, FailoverClient через Sentinel
├── internal/
│   ├── handler/handler.go # бизнес-логика и работа с Redis
│   └── server/            # echo-сервер и маршруты
├── docker-compose.yml     # стек Redis + Sentinel + приложение
├── sentinel1.conf         # конфиги Sentinel (26379/26380/26381)
├── sentinel2.conf
├── sentinel3.conf
├── seed.sh                # подготовка демо-данных через curl
├── dz1_check.py           # скрипт самопроверки
└── Dockerfile             # образ приложения
```