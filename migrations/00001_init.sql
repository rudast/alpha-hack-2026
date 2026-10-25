-- +goose Up
CREATE TABLE companies (
    inn                   text PRIMARY KEY,
    org_type              text NOT NULL CHECK (org_type IN ('IP', 'OOO')),
    name                  text NOT NULL,
    okved                 text NOT NULL DEFAULT '',
    ogrn                  text,
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

CREATE TABLE applications (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    version      integer NOT NULL DEFAULT 1, -- растёт при каждом изменении; PATCH принимает только актуальную версию (защита от перезаписи)
    company_inn  text NOT NULL REFERENCES companies (inn),
    created_by   uuid REFERENCES users (id),
    assigned_to  uuid REFERENCES users (id), -- сотрудник, ведущий заявку на ручной проверке
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
    documents    jsonb NOT NULL DEFAULT '[]',
    history      jsonb NOT NULL DEFAULT '[]',
    decision     jsonb,

    submitted_at timestamptz,
    enqueued_at  timestamptz, -- когда задача по заявке последний раз отправлялась воркеру (ставит отправка заявки и сборщик застрявших заявок)
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX applications_status_submitted_idx ON applications (status, submitted_at);
CREATE INDEX applications_company_created_idx ON applications (company_inn, created_at DESC);

-- +goose Down
DROP TABLE applications;
DROP TABLE users;
DROP TABLE companies;
