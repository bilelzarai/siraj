-- Who is waiting for a 1v1 duel (DU-01). One row per player; the pairing is done by
-- the duel service inside a transaction, so two players can never take each other
-- twice.
CREATE TABLE duel_queue (
  user_id    uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  domain_id  bigint REFERENCES domains(id) ON DELETE CASCADE,
  is_guest   boolean NOT NULL DEFAULT false,
  rating     integer NOT NULL DEFAULT 1200,
  game_id    uuid REFERENCES games(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- The oldest waiting player of the same kind is matched first.
CREATE INDEX duel_queue_waiting ON duel_queue (is_guest, domain_id, created_at)
  WHERE game_id IS NULL;
