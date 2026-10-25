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
  [ -n "${IDEM:-}" ] && args+=(-H "Idempotency-Key: $IDEM")
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

# numfield ИМЯ — достаёт числовое поле из JSON-ответа.
numfield() { echo "$BODY" | sed -n "s/.*\"$1\":\([0-9][0-9]*\).*/\1/p"; }

# newkey — новый уникальный ключ идемпотентности.
newkey() { echo "smoke-$RANDOM$RANDOM-$(date +%s%N)"; }

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

# token_for ТЕЛЕФОН — проходит оба шага входа клиента и печатает токен.
token_for() {
  login client "$1" "$PASSWORD"
  local c; c=$(field challenge_id)
  call POST /api/v1/auth/verify "{\"challenge_id\":\"$c\",\"code\":\"$CODE\"}"
  field token
}

echo "== Заявки: доступ и создание (клиент +79990000002, ООО)"
call GET /api/v1/applications;                                          check "список без токена" 401
call GET /api/v1/applications '' "$emp_token";                          check "список токеном сотрудника" 403
call POST /api/v1/applications '{"product_id":"express"}' "$token";     check "создание без Idempotency-Key" 400
IDEM=$(newkey) call POST /api/v1/applications '{"product_id":"nope"}' "$token"; check "создание: неизвестный продукт" 400
IDEM=$(newkey) call POST /api/v1/applications '{"amount":-5}' "$token";         check "создание: отрицательная сумма" 400

create_key=$(newkey)
IDEM=$create_key call POST /api/v1/applications '{"product_id":"express"}' "$token"; check "создание черновика" 201
app_id=$(field id); created_body=$BODY; ver=$(numfield version)
[ "$(field status)" = "DRAFT" ] && echo "ok    статус нового черновика: DRAFT" || { echo "FAIL  статус черновика: '$(field status)'"; fail=1; }
[ "$ver" = "1" ] && echo "ok    версия нового черновика: 1" || { echo "FAIL  версия черновика: '$ver'"; fail=1; }
call GET /api/v1/applications '' "$token"; count_after_create=$(echo "$BODY" | grep -o '"id":"' | wc -l)

echo "== Идемпотентность создания"
IDEM=$create_key call POST /api/v1/applications '{"product_id":"express"}' "$token"; check "повтор создания с тем же ключом" 201
[ "$BODY" = "$created_body" ] && echo "ok    повтор вернул тот же ответ" || { echo "FAIL  ответ повтора отличается"; fail=1; }
call GET /api/v1/applications '' "$token"
[ "$(echo "$BODY" | grep -o '"id":"' | wc -l)" = "$count_after_create" ] && echo "ok    второй черновик не создан" || { echo "FAIL  повтор создал ещё один черновик"; fail=1; }
IDEM=$create_key call POST /api/v1/applications '{"product_id":"ooo_loan"}' "$token"; check "тот же ключ для другого запроса" 422

echo "== Просмотр, сохранение и защита от перезаписи"
call GET "/api/v1/applications/$app_id" '' "$token";                    check "просмотр своей заявки" 200
IDEM=$(newkey) call POST "/api/v1/applications/$app_id/submit" '' "$token"; check "отправка неполной заявки" 422
echo "      ответ: $BODY"
patch_body="{\"version\":$ver,\"amount\":500000,\"term_months\":6,\"purpose\":\"Working capital\",\"form\":{\"applicant\":{\"position\":\"Director\",\"has_collateral\":false},\"consents\":{\"bki_request\":true,\"personal_data\":true}}}"
patch_key=$(newkey)
IDEM=$patch_key call PATCH "/api/v1/applications/$app_id" "$patch_body" "$token"; check "сохранение черновика" 200
patched_body=$BODY; ver2=$(numfield version)
[ "$ver2" = "2" ] && echo "ok    версия выросла до 2" || { echo "FAIL  версия после сохранения: '$ver2'"; fail=1; }
IDEM=$patch_key call PATCH "/api/v1/applications/$app_id" "$patch_body" "$token"; check "повтор сохранения с тем же ключом" 200
[ "$BODY" = "$patched_body" ] && echo "ok    повтор вернул тот же ответ, версия не выросла" || { echo "FAIL  повтор изменил ответ"; fail=1; }
IDEM=$(newkey) call PATCH "/api/v1/applications/$app_id" "{\"version\":$ver,\"purpose\":\"stale\"}" "$token"; check "сохранение по устаревшей версии" 409
IDEM=$(newkey) call PATCH "/api/v1/applications/$app_id" '{"purpose":"no version"}' "$token"; check "сохранение без версии" 400
call GET "/api/v1/applications/$app_id" '' "$token"
[ "$(field purpose)" = "Working capital" ] && echo "ok    устаревшее сохранение ничего не затёрло" || { echo "FAIL  данные перезаписаны: '$(field purpose)'"; fail=1; }
call GET /api/v1/applications '' "$token";                              check "список заявок" 200

echo "== Заявки: чужая компания (клиент +79990000001) не видит заявку"
token_b=$(token_for +79990000001)
call GET "/api/v1/applications/$app_id" '' "$token_b";                  check "чужая заявка: просмотр" 404
IDEM=$(newkey) call PATCH "/api/v1/applications/$app_id" '{"version":2,"purpose":"foreign"}' "$token_b"; check "чужая заявка: изменение" 404
IDEM=$(newkey) call POST "/api/v1/applications/$app_id/submit" '' "$token_b"; check "чужая заявка: отправка" 404
call GET /api/v1/applications '' "$token_b";                            check "список другой компании" 200
case "$BODY" in
  *"$app_id"*) echo "FAIL  чужая заявка попала в список"; fail=1 ;;
  *) echo "ok    чужая заявка в списке не видна" ;;
esac
call GET /api/v1/applications/not-a-uuid '' "$token";                   check "некорректный идентификатор в адресе" 404

echo "== Заявки: отправка"
submit_key=$(newkey)
IDEM=$submit_key call POST "/api/v1/applications/$app_id/submit" '' "$token"; check "отправка полной заявки" 200
submitted_body=$BODY
[ "$(field status)" = "SUBMITTED" ] && echo "ok    статус после отправки: SUBMITTED" || { echo "FAIL  статус после отправки: '$(field status)'"; fail=1; }
IDEM=$submit_key call POST "/api/v1/applications/$app_id/submit" '' "$token"; check "повтор отправки с тем же ключом" 200
[ "$BODY" = "$submitted_body" ] && echo "ok    повтор вернул тот же ответ" || { echo "FAIL  ответ повтора отличается"; fail=1; }
IDEM=$(newkey) call POST "/api/v1/applications/$app_id/submit" '' "$token"; check "отправка ещё раз с новым ключом" 409
IDEM=$(newkey) call PATCH "/api/v1/applications/$app_id" '{"version":3,"purpose":"after submit"}' "$token"; check "изменение после отправки" 409

echo "== Заявки: продукт недоступен компании (клиент +79990000005, ИП, продукт только для ООО)"
token_ip=$(token_for +79990000005)
IDEM=$(newkey) call POST /api/v1/applications '{"product_id":"ooo_loan","amount":500000,"term_months":6,"purpose":"Purchase","form":{"applicant":{"position":"IP","has_collateral":false},"consents":{"bki_request":true}}}' "$token_ip"
check "создание черновика" 201
IDEM=$(newkey) call POST "/api/v1/applications/$(field id)/submit" '' "$token_ip"; check "отправка недоступного продукта" 422
echo "      ответ: $BODY"

echo "== Блокировка после трёх неудач (случайный несуществующий номер, чтобы не блокировать демо-аккаунты)"
phone="+7999$(printf '%07d' $(( (RANDOM * 32768 + RANDOM) % 10000000 )))"
for i in 1 2 3; do login client "$phone" wrong; check "попытка $i" 401; done
login client "$phone" "$PASSWORD"; check "четвёртая попытка заблокирована" 429

echo
if [ "$fail" = 0 ]; then echo "Все проверки прошли."; else echo "Есть ошибки."; fi
exit $fail
