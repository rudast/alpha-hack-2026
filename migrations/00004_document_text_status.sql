-- +goose Up
ALTER TABLE application_documents DROP CONSTRAINT application_documents_status_check;
ALTER TABLE application_documents ADD CONSTRAINT application_documents_status_check
    CHECK (status IN ('UPLOADED', 'PARSED', 'TEXT_EXTRACTED', 'UNSUPPORTED', 'FAILED'));

-- +goose Down
UPDATE application_documents SET status = 'UNSUPPORTED' WHERE status = 'TEXT_EXTRACTED';
ALTER TABLE application_documents DROP CONSTRAINT application_documents_status_check;
ALTER TABLE application_documents ADD CONSTRAINT application_documents_status_check
    CHECK (status IN ('UPLOADED', 'PARSED', 'UNSUPPORTED', 'FAILED'));
