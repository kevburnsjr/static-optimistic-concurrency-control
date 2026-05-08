CREATE TABLE accounts (
  "id" INTEGER PRIMARY KEY,
  "version" INTEGER NOT NULL,
  "balance" INTEGER NOT NULL
);

CREATE OR REPLACE FUNCTION occ_write_check()
  RETURNS TRIGGER AS $$
  BEGIN
    IF (NEW.version != OLD.version) THEN
      RAISE EXCEPTION 'VERSION_CONFLICT' USING ERRCODE='OC000';
    ELSE
      NEW.version := NEW.version + 1;
    END IF;
    RETURN NEW;
  END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER tg_occ_write_check BEFORE UPDATE ON accounts
  FOR EACH ROW EXECUTE PROCEDURE occ_write_check();
