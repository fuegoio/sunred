-- Indexes supporting per-article repost (share) and star counts embedded in
-- the /entries list response. The list query correlates these tables against
-- each entry's article URL; without an index on article_url the per-row
-- COUNT(DISTINCT user_id) would be a sequential scan, multiplied across
-- every entry in the page. These partial-ish indexes cover the count paths
-- without constraining the existing (user_id, article_url) primary keys.

CREATE INDEX IF NOT EXISTS idx_shared_articles_article_url ON shared_articles (article_url);
CREATE INDEX IF NOT EXISTS idx_entry_stars_article_url     ON entry_stars (article_url);
