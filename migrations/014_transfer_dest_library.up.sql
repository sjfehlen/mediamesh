ALTER TABLE transfers ADD COLUMN dest_library_id TEXT REFERENCES libraries(id);
