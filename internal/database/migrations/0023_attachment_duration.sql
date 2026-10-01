-- How long a voice note runs.
--
-- MediaRecorder writes a WebM stream with no duration in its header: the file
-- does not know how long it is, so every player shows 0:00 / 0:00 for a
-- recording that lasted seven seconds, and there is nothing in the bytes to
-- read it back from. The only place the length is known is the browser that
-- was holding the microphone, which measures it and sends it along.
--
-- Nullable because it is unknowable for everything else: a photograph has no
-- duration, and a recording sent by a client that did not measure one is not a
-- recording of zero seconds.
ALTER TABLE attachments ADD COLUMN duration_ms integer
    CHECK (duration_ms IS NULL OR duration_ms > 0);
