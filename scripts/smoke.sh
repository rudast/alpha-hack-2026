#!/usr/bin/env bash
# Дымовая проверка всех обработчиков API через curl.
# Запуск: bash scripts/smoke.sh [адрес], по умолчанию http://localhost:8080.
# Нужен запущенный стек (docker compose up) с демо-данными из миграции 00002.
# Код возврата: 0 если все проверки прошли, 1 если хоть одна нет.

set -u
BASE=${1:-http://localhost:8080}
PASSWORD='demo-password'
CODE='000000'
fail=0
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

# call МЕТОД ПУТЬ [ТЕЛО] [ТОКЕН] — выполняет запрос, результат в STATUS и BODY.
call() {
  local method=$1 path=$2 body=${3:-} token=${4:-}
  local args=(-s -o "$tmp" -w '%{http_code}' -X "$method" "$BASE$path")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")
  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  STATUS=$(curl "${args[@]}")
  BODY=$(cat "$tmp")
}

# check ОПИСАНИЕ ОЖИДАЕМЫЙ_КОД — сверяет код ответа последнего запроса.
check() {
  if [ "$STATUS" = "$2" ]; then
    echo "ok    $1 ($STATUS)"
  else
    echo "FAIL  $1: ожидали $2, получили $STATUS: $BODY"
    fail=1
  fi
}

# field ИМЯ — достаёт строковое поле из JSON-ответа.
field() { echo "$BODY" | sed -n "s/.*\"$1\":\"\([^\"]*\)\".*/\1/p"; }

# login РОЛЬ ЛОГИН ПАРОЛЬ — шаг 1 входа.
login() { call POST /api/v1/auth/login "{\"role\":\"$1\",\"login\":\"$2\",\"password\":\"$3\"}"; }

echo "== Служебные"
call GET /healthz;  check "GET /healthz" 200
call GET /;         check "GET / (статика)" 200

echo "== Вход: ошибки"
call POST /api/v1/auth/login 'не json';                      check "login: некорректное тело" 400
login client +79990000002 wrong;                             check "login: неверный пароль" 401
login client +79990009999 "$PASSWORD";                       check "login: неизвестный телефон" 401
login client underwriter@example.test "$PASSWORD";           check "login: почта сотрудника в роли клиента" 401
login admin +79990000002 "$PASSWORD";                        check "login: неизвестная роль" 401
call POST /api/v1/auth/verify '{"challenge_id":"nope","code":"000000"}'; check "verify: неизвестный challenge" 401

echo "== Вход клиента"
login client +79990000002 "$PASSWORD";   check "login клиента по телефону" 200
challenge=$(field challenge_id)
call POST /api/v1/auth/verify "{\"challenge_id\":\"$challenge\",\"code\":\"111111\"}"; check "verify: неверный код" 401
call POST /api/v1/auth/verify "{\"challenge_id\":\"$challenge\",\"code\":\"$CODE\"}";   check "verify: верный код" 200
token=$(field token); redirect=$(field redirect)
[ "$redirect" = "/personalAccount" ] && echo "ok    redirect клиента: $redirect" || { echo "FAIL  redirect клиента: '$redirect'"; fail=1; }
call POST /api/v1/auth/verify "{\"challenge_id\":\"$challenge\",\"code\":\"$CODE\"}";   check "verify: повторное использование challenge" 401

echo "== Вход клиента по почте"
login client client2@example.test "$PASSWORD"; check "login клиента по почте" 200
call POST /api/v1/auth/verify "{\"challenge_id\":\"$(field challenge_id)\",\"code\":\"$CODE\"}"; check "verify по почте" 200

echo "== /me/bootstrap"
call POST /api/v1/me/bootstrap;                  check "bootstrap без токена" 401
call POST /api/v1/me/bootstrap '' bad-token;     check "bootstrap с неверным токеном" 401
call POST /api/v1/me/bootstrap '' "$token";      check "bootstrap клиента" 200
echo "      ответ: $BODY"

echo "== Вход сотрудника"
login employee underwriter@example.test "$PASSWORD"; check "login сотрудника по почте" 200
call POST /api/v1/auth/verify "{\"challenge_id\":\"$(field challenge_id)\",\"code\":\"$CODE\"}"; check "verify сотрудника" 200
emp_token=$(field token); redirect=$(field redirect)
[ "$redirect" = "/employee" ] && echo "ok    redirect сотрудника: $redirect" || { echo "FAIL  redirect сотрудника: '$redirect'"; fail=1; }
call POST /api/v1/me/bootstrap '' "$emp_token"; check "bootstrap сотрудником (только для клиентов)" 403
login employee +79990000100 "$PASSWORD";        check "login сотрудника по телефону" 200

echo "== Блокировка после трёх неудач (случайный несуществующий номер, чтобы не блокировать демо-аккаунты)"
phone="+7999$(printf '%07d' $(( (RANDOM * 32768 + RANDOM) % 10000000 )))"
for i in 1 2 3; do login client "$phone" wrong; check "попытка $i" 401; done
login client "$phone" "$PASSWORD"; check "четвёртая попытка заблокирована" 429

echo
if [ "$fail" = 0 ]; then echo "Все проверки прошли."; else echo "Есть ошибки."; fi
exit $fail
