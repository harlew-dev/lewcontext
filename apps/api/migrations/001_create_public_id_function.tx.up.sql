-- public_id(prefix) returns prefix_ plus 14 random base62 characters, e.g.
-- prj_j292s2S2j7Qx4B (about 83 random bits). Tables use it as a column default,
-- so every insert gets an ID without the application generating one.
-- gen_random_uuid() is built in (PostgreSQL 13+) and uses the system's secure
-- random source; no extension is needed.
CREATE FUNCTION public_id(prefix text) RETURNS text
LANGUAGE plpgsql VOLATILE AS $$
DECLARE
 alphabet constant text := '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
 id text := '';
 bytes bytea;
 b int;
BEGIN
 WHILE length(id) < 14 LOOP
  bytes := uuid_send(gen_random_uuid());
  FOR i IN 0..15 LOOP
   -- Bytes 6 and 8 hold the UUID's fixed version and variant bits.
   CONTINUE WHEN i IN (6, 8);
   b := get_byte(bytes, i);
   -- 248 = 4 × 62: dropping 248–255 keeps every character equally likely.
   CONTINUE WHEN b >= 248;
   id := id || substr(alphabet, b % 62 + 1, 1);
   EXIT WHEN length(id) = 14;
  END LOOP;
 END LOOP;
 RETURN prefix || '_' || id;
END $$;
