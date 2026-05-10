-- Create database and user for sub2api
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'sub2api') THEN
        CREATE ROLE sub2api WITH LOGIN PASSWORD 'sub2api';
    END IF;
END
$$;

SELECT 'CREATE DATABASE sub2api'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'sub2api')\gexec

GRANT ALL PRIVILEGES ON DATABASE sub2api TO sub2api;
