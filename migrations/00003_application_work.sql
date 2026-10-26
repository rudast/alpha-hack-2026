-- +goose Up
CREATE TABLE application_documents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    kind text NOT NULL,
    filename text NOT NULL,
    media_type text NOT NULL,
    content bytea NOT NULL,
    status text NOT NULL DEFAULT 'UPLOADED' CHECK (status IN ('UPLOADED', 'PARSED', 'UNSUPPORTED', 'FAILED')),
    extracted jsonb NOT NULL DEFAULT '{}',
    error text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX application_documents_application_idx ON application_documents(application_id);

CREATE TABLE worker_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('verify_application', 'parse_documents', 'critical_risk_review')),
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'QUEUED' CHECK (status IN ('QUEUED', 'RUNNING', 'DONE', 'FAILED')),
    result jsonb,
    error text,
    enqueued_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX worker_jobs_dispatch_idx ON worker_jobs(status, enqueued_at) WHERE status = 'QUEUED';
CREATE INDEX worker_jobs_application_idx ON worker_jobs(application_id, created_at);

-- +goose Down
DROP TABLE worker_jobs;
DROP TABLE application_documents;
