-- +goose Up
CREATE TABLE companies (
    inn                   text PRIMARY KEY,
    org_type              text NOT NULL CHECK (org_type IN ('IP', 'OOO')),
    name                  text NOT NULL,
    okved                 text NOT NULL DEFAULT '',
    okved_title           text,                       -- расшифровка ОКВЭД
    ogrn                  text,
    account_number        text,                       -- расчётный счёт в нашем банке
    registration_date     date,

    -- Показатели долговой нагрузки из кредитной истории.
    -- financials_fetched_at IS NULL означает, что данные ещё ни разу не запрашивались.
    -- debt_to_equity равен NULL, если собственный капитал нулевой или отрицательный (коэффициент не определён).
    total_debt            bigint,
    equity                bigint,
    debt_to_equity        numeric(12, 4),
    financials_fetched_at timestamptz,

    registry              jsonb NOT NULL DEFAULT '{}', -- ответ ЕГРЮЛ/ЕГРИП и «Прозрачного бизнеса»
    credit_history        jsonb NOT NULL DEFAULT '{}', -- ответ БКИ
    financials            jsonb NOT NULL DEFAULT '{}', -- дополнительные данные компании, например помесячные обороты

    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT companies_inn_format_check CHECK (
        (org_type = 'OOO' AND inn ~ '^[0-9]{10}$') OR (org_type = 'IP' AND inn ~ '^[0-9]{12}$')
    )
);

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name     text NOT NULL DEFAULT '',
    position      text,          -- должность клиента в компании
    phone         text UNIQUE,
    email         text,
    password_hash text NOT NULL,
    role          text NOT NULL CHECK (role IN ('client', 'employee')),
    company_inn   text REFERENCES companies (inn),
    is_active     boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),

    -- Клиент принадлежит компании, сотрудник нет.
    CONSTRAINT users_company_check CHECK ((role = 'client') = (company_inn IS NOT NULL)),
    -- Клиент входит по телефону, сотрудник по почте; второй логин необязателен.
    CONSTRAINT users_login_identifier_check CHECK (
        (role = 'client' AND phone IS NOT NULL) OR (role = 'employee' AND email IS NOT NULL)
    ),
    CONSTRAINT users_phone_format_check CHECK (phone ~ '^\+7[0-9]{10}$')
);

CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

-- Предварительные предложения (баннер в кабинете), docs/preapproved-offer.md. Предложение фиксируется
-- при расчёте: заявка по нему берёт сумму, срок и продукт отсюда, а не из запроса клиента.
CREATE TABLE offers (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_inn text NOT NULL REFERENCES companies (inn),
    product_id  text NOT NULL,
    max_amount  bigint NOT NULL CHECK (max_amount > 0),
    term_months integer NOT NULL CHECK (term_months > 0),
    rate_from   numeric(5, 2) NOT NULL,
    status      text NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'USED', 'EXPIRED', 'REVOKED')),
    basis       jsonb NOT NULL,           -- снимок данных компании, по которым посчитано предложение
    valid_until timestamptz NOT NULL,     -- задаётся при расчёте и дальше не меняется
    created_at  timestamptz NOT NULL DEFAULT now(),
    used_at     timestamptz
);

-- У компании не больше одного действующего предложения.
CREATE UNIQUE INDEX offers_one_active_idx ON offers (company_inn) WHERE status = 'ACTIVE';

CREATE TABLE applications (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    version      integer NOT NULL DEFAULT 1, -- растёт при каждом изменении; PATCH принимает только актуальную версию (защита от перезаписи)
    company_inn  text NOT NULL REFERENCES companies (inn),
    created_by   uuid REFERENCES users (id),
    assigned_to  uuid REFERENCES users (id), -- сотрудник, ведущий заявку на ручной проверке
    offer_id     uuid REFERENCES offers (id), -- предварительное предложение, по которому создана заявка
    product_id   text NOT NULL DEFAULT '',   -- идентификатор из каталога продуктов
    amount       bigint NOT NULL DEFAULT 0 CHECK (amount >= 0),
    term_months  integer NOT NULL DEFAULT 0 CHECK (term_months >= 0),
    purpose      text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'DRAFT' CHECK (status IN (
        'DRAFT', 'SUBMITTED', 'DATA_CHECK', 'INFO_REQUESTED', 'SCORING',
        'MANUAL_REVIEW', 'APPROVED', 'COUNTER_OFFER', 'REJECTED', 'SIGNING',
        'SIGNED', 'DISBURSED', 'ACTIVE', 'OVERDUE', 'CLOSED'
    )),

    -- Данные формы по группам полей: form.<группа>.<поле>. Состав зависит от продукта
    -- и описан в каталоге продуктов, проверка полноты делается в коде.
    form         jsonb NOT NULL DEFAULT '{}',
    history      jsonb NOT NULL DEFAULT '[]',
    decision     jsonb,

    -- Выданный кредит. Отдельной таблицы кредитов нет: заявка после выдачи (ACTIVE, OVERDUE, CLOSED)
    -- и есть кредит. amount и term_months у выданного кредита — одобренные сумма и срок. График платежей
    -- не хранится, а считается из суммы, ставки, срока, даты выдачи и числа внесённых платежей.
    rate            numeric(5, 2),  -- ставка выданного кредита, % годовых
    contract_number text,
    issued_at       date,           -- дата выдачи; её день месяца — день платежа
    paid_payments   integer NOT NULL DEFAULT 0 CHECK (paid_payments >= 0), -- сколько ежемесячных платежей внесено
    late_payments   integer NOT NULL DEFAULT 0 CHECK (late_payments >= 0), -- сколько из них внесено с опозданием
    closed_at       date,

    submitted_at timestamptz,
    enqueued_at  timestamptz, -- когда задача по заявке последний раз отправлялась воркеру (ставит отправка заявки и сборщик застрявших заявок)
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX applications_status_submitted_idx ON applications (status, submitted_at);
CREATE INDEX applications_company_created_idx ON applications (company_inn, created_at DESC);
-- По одному предложению создаётся не больше одной заявки: повторное «Оформить» возвращает её же.
CREATE UNIQUE INDEX applications_offer_idx ON applications (offer_id) WHERE offer_id IS NOT NULL;

-- +goose Down
DROP TABLE applications;
DROP TABLE offers;
DROP TABLE users;
DROP TABLE companies;
