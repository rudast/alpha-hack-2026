-- +goose Up
CREATE TABLE companies (
    inn        text PRIMARY KEY,
    org_type   text NOT NULL CHECK (org_type IN ('IP', 'OOO')),
    name       text NOT NULL,
    okved      text NOT NULL DEFAULT '',
    financials jsonb NOT NULL DEFAULT '{}'
);

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    role          text NOT NULL CHECK (role IN ('client', 'employee')),
    company_inn   text REFERENCES companies (inn),
    CHECK ((role = 'client') = (company_inn IS NOT NULL))
);

CREATE TABLE applications (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_inn text NOT NULL REFERENCES companies (inn),
    product_id  text NOT NULL DEFAULT '',
    amount      bigint NOT NULL DEFAULT 0 CHECK (amount >= 0),
    term_months integer NOT NULL DEFAULT 0 CHECK (term_months >= 0),
    purpose     text NOT NULL DEFAULT '',
    status      text NOT NULL DEFAULT 'DRAFT' CHECK (status IN (
        'DRAFT', 'SUBMITTED', 'DATA_CHECK', 'INFO_REQUESTED', 'SCORING',
        'MANUAL_REVIEW', 'APPROVED', 'COUNTER_OFFER', 'REJECTED', 'SIGNING',
        'SIGNED', 'DISBURSED', 'ACTIVE', 'OVERDUE', 'CLOSED'
    )),
    history     jsonb NOT NULL DEFAULT '[]',
    documents   jsonb NOT NULL DEFAULT '[]',
    decision    jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX applications_company_inn_idx ON applications (company_inn);
CREATE INDEX applications_status_idx ON applications (status);

-- +goose Down
DROP TABLE applications;
DROP TABLE users;
DROP TABLE companies;
