-- =============================================================================
-- 0004_support.sql — support conversations between players and staff
-- =============================================================================

-- What the player is writing about. The kind is what lets staff triage a
-- queue without reading every message first.
CREATE TYPE ticket_kind AS ENUM (
    'suggestion',   -- an idea for the app
    'question',     -- a proposed or disputed quiz question
    'bug',          -- something is broken
    'account',      -- login, profile, data
    'abuse',        -- reporting another player
    'other'
);

CREATE TYPE ticket_status AS ENUM (
    'open',          -- waiting on staff
    'in_progress',   -- staff is working on it
    'waiting_user',  -- staff replied, waiting on the player
    'resolved',      -- done, player can still reopen
    'closed'         -- archived
);

CREATE TYPE ticket_priority AS ENUM ('low', 'normal', 'high', 'urgent');

CREATE TABLE support_tickets (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind        ticket_kind     NOT NULL DEFAULT 'other',
    status      ticket_status   NOT NULL DEFAULT 'open',
    priority    ticket_priority NOT NULL DEFAULT 'normal',
    subject     text NOT NULL,
    locale      text NOT NULL DEFAULT 'ar',

    -- Optional context, so a content suggestion arrives already attached to
    -- the thing it is about instead of described in prose.
    category_id integer REFERENCES categories(id) ON DELETE SET NULL,
    question_id integer REFERENCES questions(id) ON DELETE SET NULL,
    page_path   text NOT NULL DEFAULT '',

    assigned_to uuid REFERENCES users(id) ON DELETE SET NULL,

    -- Denormalised so an inbox listing does not need a per-row subquery.
    last_message_at timestamptz NOT NULL DEFAULT now(),
    last_sender     text NOT NULL DEFAULT 'user',
    user_unread     integer NOT NULL DEFAULT 0,
    staff_unread    integer NOT NULL DEFAULT 1,
    message_count   integer NOT NULL DEFAULT 0,

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,

    CHECK (btrim(subject) <> '')
);

-- The inbox is filtered by status, kind, priority and assignee, so each of
-- those gets an index that supports the newest-first ordering.
CREATE INDEX support_tickets_queue_idx    ON support_tickets (status, priority DESC, last_message_at DESC);
CREATE INDEX support_tickets_kind_idx     ON support_tickets (kind, last_message_at DESC);
CREATE INDEX support_tickets_assignee_idx ON support_tickets (assigned_to, last_message_at DESC);
CREATE INDEX support_tickets_user_idx     ON support_tickets (user_id, last_message_at DESC);
CREATE INDEX support_tickets_subject_trgm ON support_tickets USING gin (subject gin_trgm_ops);

-- Awaiting-reply is the queue staff actually work from.
CREATE INDEX support_tickets_waiting_idx
    ON support_tickets (last_message_at DESC)
    WHERE status IN ('open', 'in_progress');

CREATE TABLE support_messages (
    id         bigserial PRIMARY KEY,
    ticket_id  uuid NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    sender_id  uuid REFERENCES users(id) ON DELETE SET NULL,
    -- 'user' or 'staff'. Kept denormalised so a deleted staff account does
    -- not turn their replies into anonymous player messages.
    sender_role text NOT NULL CHECK (sender_role IN ('user', 'staff', 'system')),
    body       text NOT NULL,
    -- An internal note is visible to staff only; the player never sees it.
    internal   boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),

    CHECK (btrim(body) <> ''),
    CHECK (NOT internal OR sender_role = 'staff')
);

CREATE INDEX support_messages_thread_idx ON support_messages (ticket_id, id);

-- Staff-visible saved replies, so common answers stay consistent.
CREATE TABLE support_canned_replies (
    id         serial PRIMARY KEY,
    locale     text NOT NULL,
    title      text NOT NULL,
    body       text NOT NULL,
    sort_order integer NOT NULL DEFAULT 0
);

INSERT INTO support_canned_replies (locale, title, body, sort_order) VALUES
    ('en', 'Thanks for the suggestion',
     'Thank you for taking the time to write in. We have added your suggestion to our list and will let you know here if it is picked up.', 1),
    ('en', 'Question corrected',
     'You were right — the question has been corrected. Thank you for reporting it.', 2),
    ('en', 'Need more detail',
     'Could you tell us which screen you were on and what you expected to happen? That will help us reproduce it.', 3),
    ('fr', 'Merci pour la suggestion',
     'Merci d''avoir pris le temps de nous écrire. Votre suggestion a été ajoutée à notre liste et nous vous tiendrons informé ici.', 1),
    ('fr', 'Question corrigée',
     'Vous aviez raison — la question a été corrigée. Merci de nous l''avoir signalée.', 2),
    ('fr', 'Précisions nécessaires',
     'Pourriez-vous nous indiquer sur quel écran vous étiez et ce que vous attendiez ? Cela nous aidera à reproduire le problème.', 3),
    ('ar', 'شكرًا على الاقتراح',
     'شكرًا لتواصلك معنا. أضفنا اقتراحك إلى قائمتنا وسنخبرك هنا إن تم الأخذ به.', 1),
    ('ar', 'تم تصحيح السؤال',
     'كنت محقًا — تم تصحيح السؤال. شكرًا لإبلاغنا.', 2),
    ('ar', 'نحتاج تفاصيل أكثر',
     'هل يمكنك إخبارنا بالشاشة التي كنت فيها وما الذي توقعت حدوثه؟ سيساعدنا ذلك على إعادة المشكلة.', 3);
