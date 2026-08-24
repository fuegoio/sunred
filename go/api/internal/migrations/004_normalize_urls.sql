-- Normalize URL identity columns in place.
--
-- star/read/share state is keyed by article_url and feeds are keyed by
-- feed_url. Before this change those columns stored the raw URL, so the
-- same article/feed reached via two textually-different URLs (http vs
-- https, trailing slash, fragment, tracking query params, query order,
-- default port, host casing) produced distinct rows and divergent state.
--
-- Going forward, the API normalizes every URL before writing (see
-- internal/urlnorm). This migration backfills existing rows to the same
-- canonical form and merges the duplicates the normalization creates.
--
-- Order matters: feeds and the per-user state tables carry a unique
-- constraint on their URL column, so we cannot normalize in place while
-- two rows still differ textually but collapse to one canonical form
-- (the UPDATE would hit the unique constraint). We therefore dedup by
-- canonical value first — merging feeds and keeping the newest state row
-- per (user, article_url) — and only then normalize the survivors in
-- place, when no constraint can fire.

-- Canonicalize a URL: lowercase scheme + host, drop default ports, drop
-- fragment, strip trailing slash (except root), drop tracking query
-- params (utm_* and a known set), sort the surviving params by key.
-- Non-absolute or unparseable input is returned unchanged, matching the
-- Go behaviour for relative/invalid URLs.
CREATE OR REPLACE FUNCTION sunred_canonical_url(raw text) RETURNS text AS $$
DECLARE
  s      text;
  scheme text;
  rest   text;
  auth   text;
  path   text;
  host   text;
  port   text;
  query  text;
  qparts text[];
  kept   text[] := ARRAY[]::text[];
  kv     text;
  k      text;
  i      int;
  sorted text[];
  out    text;
BEGIN
  IF raw IS NULL THEN RETURN ''; END IF;
  s := btrim(raw);
  IF s = '' THEN RETURN ''; END IF;

  -- Only rewrite absolute URLs (scheme://...). Anything else is left as-is.
  IF s !~ '^[a-zA-Z][a-zA-Z0-9+.-]*://' THEN
    RETURN raw;
  END IF;

  scheme := lower(substring(s from '^([a-zA-Z][a-zA-Z0-9+.-]*)://'));
  rest  := substring(s from '^[a-zA-Z][a-zA-Z0-9+.-]*://(.*)$');

  -- Strip fragment.
  rest := split_part(rest, '#', 1);

  -- Split query off the rest.
  query := '';
  IF position('?' in rest) > 0 THEN
    query := substring(rest from '\?(.*)$');
    rest  := substring(rest from '^([^?]*)');
  END IF;

  -- Split authority and path at the first '/'.
  IF position('/' in rest) > 0 THEN
    auth := substring(rest from '^([^/]*)');
    path := substring(rest from '(/.*)$');
  ELSE
    auth := rest;
    path := '';
  END IF;

  -- Lowercase host, drop default port.
  host := lower(substring(auth from '^([^:]*)'));
  port := substring(auth from ':(.*)$');
  IF port <> '' THEN
    IF NOT ((scheme = 'https' AND port = '443') OR
           (scheme = 'http'  AND port = '80')) THEN
      host := host || ':' || port;
    END IF;
  END IF;

  -- Strip trailing slash from non-root paths.
  IF length(path) > 1 THEN
    path := rtrim(path, '/');
  END IF;

  -- Normalize the query: drop tracking params, sort survivors by key.
  IF query <> '' THEN
    qparts := string_to_array(query, '&');
    FOR i IN array_lower(qparts, 1) .. array_upper(qparts, 1) LOOP
      kv := qparts[i];
      IF kv = '' THEN CONTINUE; END IF;
      k := lower(split_part(kv, '=', 1));
      IF k LIKE 'utm_%' OR k IN (
        'fbclid','gclid','igshid','mc_cid','mc_eid','ref','ref_src','ref_url',
        'source','yclid','msclkid','_hsenc','_hsmi','spm','ito','cid','si'
      ) THEN
        CONTINUE;
      END IF;
      kept := array_append(kept, kv);
    END LOOP;
    SELECT array_agg(x ORDER BY lower(split_part(x, '=', 1))) INTO sorted
      FROM unnest(kept) AS x;
    query := array_to_string(sorted, '&');
  END IF;

  out := scheme || '://' || host || path;
  IF query <> '' THEN
    out := out || '?' || query;
  END IF;
  RETURN out;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- --- Dedup feeds by canonical feed_url (before normalizing) ---
--
-- The survivor is the lowest-id feed. Losing entries that collide on
-- (feed_id, hash) against the survivor are dropped (duplicate content);
-- state tables are keyed by article_url and reference entry_id only as a
-- cache (ON DELETE SET NULL), so dropping them is safe. The merge map is
-- materialized into a temp table so several statements can reference it.
CREATE TEMP TABLE feed_merge (dup_id int, survivor_id int) ON COMMIT DROP;

INSERT INTO feed_merge (dup_id, survivor_id)
SELECT id, MIN(id) OVER (PARTITION BY sunred_canonical_url(feed_url))
  FROM feeds;

DELETE FROM feed_merge WHERE dup_id = survivor_id;

-- Move subscriptions, honouring the (user_id, feed_id) unique constraint.
UPDATE subscriptions s
   SET feed_id = fm.survivor_id, updated_at = NOW()
  FROM feed_merge fm
 WHERE s.feed_id = fm.dup_id
   AND NOT EXISTS (
     SELECT 1 FROM subscriptions s2
      WHERE s2.user_id = s.user_id AND s2.feed_id = fm.survivor_id
   );

-- Drop subscriptions that would collide with an existing survivor subscription.
DELETE FROM subscriptions s
  USING feed_merge fm
 WHERE s.feed_id = fm.dup_id;

-- Drop entries on losing feeds whose hash already exists on the survivor.
DELETE FROM entries e
  USING feed_merge fm
 WHERE e.feed_id = fm.dup_id
   AND e.hash IN (SELECT e2.hash FROM entries e2 WHERE e2.feed_id = fm.survivor_id);

-- Move the remaining entries to the survivor feed.
UPDATE entries e
   SET feed_id = fm.survivor_id
  FROM feed_merge fm
 WHERE e.feed_id = fm.dup_id;

-- Drop icons on losing feeds (feed_icons is keyed by feed_id; the survivor
-- keeps its own and the duplicates cannot be merged).
DELETE FROM feed_icons fi
  USING feed_merge fm
 WHERE fi.feed_id = fm.dup_id;

-- Delete the now-empty losing feeds.
DELETE FROM feeds f
  USING feed_merge fm
 WHERE f.id = fm.dup_id;

DROP TABLE feed_merge;

-- --- Dedup per-user state rows by canonical article_url (before
-- normalizing). Keep one row per (user_id, canonical) — the newest by the
-- table's timestamp, tiebroken by ctid for determinism. entry_id is only a
-- cache, so dropping a row never loses star/read state. ---

DELETE FROM entry_stars es
 WHERE es.ctid NOT IN (
   SELECT DISTINCT ON (user_id, sunred_canonical_url(article_url)) es2.ctid
     FROM entry_stars es2
    ORDER BY user_id, sunred_canonical_url(article_url), starred_at DESC, ctid DESC
 );

DELETE FROM entry_read_status rs
 WHERE rs.ctid NOT IN (
   SELECT DISTINCT ON (user_id, sunred_canonical_url(article_url)) rs2.ctid
     FROM entry_read_status rs2
    ORDER BY user_id, sunred_canonical_url(article_url), changed_at DESC, ctid DESC
 );

DELETE FROM shared_articles sa
 WHERE sa.ctid NOT IN (
   SELECT DISTINCT ON (user_id, sunred_canonical_url(article_url)) sa2.ctid
     FROM shared_articles sa2
    ORDER BY user_id, sunred_canonical_url(article_url), shared_at DESC, ctid DESC
 );

-- --- Normalize the survivors in place. No unique constraint can fire now:
-- each canonical group has exactly one row. ---

UPDATE feeds SET feed_url = sunred_canonical_url(feed_url), updated_at = updated_at
 WHERE feed_url <> sunred_canonical_url(feed_url);

UPDATE entries SET url = sunred_canonical_url(url)
 WHERE url <> sunred_canonical_url(url);

UPDATE entry_stars SET article_url = sunred_canonical_url(article_url)
 WHERE article_url <> sunred_canonical_url(article_url);

UPDATE entry_read_status SET article_url = sunred_canonical_url(article_url)
 WHERE article_url <> sunred_canonical_url(article_url);

UPDATE shared_articles SET article_url = sunred_canonical_url(article_url)
 WHERE article_url <> sunred_canonical_url(article_url);

DROP FUNCTION sunred_canonical_url(text);
