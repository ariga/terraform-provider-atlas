ALTER TABLE pets ADD owner_id BIGINT(20) NULL,
  ADD CONSTRAINT pets_users_id_fk
  FOREIGN KEY (owner_id) REFERENCES users (id) ON DELETE SET NULL;
