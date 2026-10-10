-- Данные для нагрузочного сценария (срез 27): что спрашивать и кем.
-- Запускает scripts/load-test на базе scibox_load перед прогоном:
--   psql -q -At -v ON_ERROR_STOP=1 -v applicants=4000 -v staff=300 -f loadtest/fixtures.sql > fixtures.json
-- Создаёт живые сеансы (cookie берётся из строки token_hash, значение токена известно сценарию), поэтому вход под
-- нагрузкой — отдельный вид запроса, а не подготовка. Сеансы старых прогонов удаляются (user_agent = 'loadtest').
-- Печатает один JSON-объект.
\set ON_ERROR_STOP on
SET client_min_messages = warning;

DELETE FROM sessions WHERE user_agent = 'loadtest';

-- Учёные: с заполненным профилем, не сотрудники организаций.
CREATE TEMP TABLE lt_applicants AS
SELECT row_number() OVER ()::int AS n, a.id AS user_id, a.email
FROM (
  SELECT u.id, u.email
  FROM users u
  JOIN profiles p ON p.user_id = u.id
  WHERE p.headline <> ''
    AND NOT EXISTS (SELECT 1 FROM org_members m WHERE m.user_id = u.id)
  ORDER BY random()
  LIMIT :applicants
) a;

-- Сотрудники: владельцы самых больших по откликам организаций и случайных остальных.
CREATE TEMP TABLE lt_staff AS
WITH big AS (
  SELECT v.org_id, count(*) AS c
  FROM applications a JOIN vacancies v ON v.id = a.vacancy_id
  GROUP BY v.org_id ORDER BY c DESC LIMIT 20
), pick AS (
  SELECT org_id FROM big
  UNION
  (SELECT id FROM organizations ORDER BY random() LIMIT :staff)
), owners AS (
  SELECT DISTINCT ON (m.org_id) m.org_id, m.user_id
  FROM org_members m JOIN pick p ON p.org_id = m.org_id
  WHERE m.role = 'owner'
  ORDER BY m.org_id, m.joined_at
)
SELECT row_number() OVER ()::int AS n, org_id, user_id FROM owners;

INSERT INTO sessions (user_id, token_hash, created_at, last_seen_at, expires_at, user_agent, ip)
SELECT user_id, sha256(convert_to('lt-a-' || n, 'UTF8')), now(), now(), now() + interval '2 days', 'loadtest', '10.9.9.9' FROM lt_applicants
UNION ALL
SELECT user_id, sha256(convert_to('lt-s-' || n, 'UTF8')), now(), now(), now() + interval '2 days', 'loadtest', '10.9.9.9' FROM lt_staff;

-- Открытые вакансии, на которые откликаются: нумерация, чтобы пары «учёный — вакансия» не повторялись.
CREATE TEMP TABLE lt_open AS
SELECT row_number() OVER (ORDER BY random())::int AS n, v.id
FROM vacancies v
WHERE v.status = 'published' AND (v.deadline IS NULL OR v.deadline >= current_date)
LIMIT 20000;

SELECT json_build_object(
  'words', (
    SELECT coalesce(json_agg(w), '[]'::json) FROM (
      SELECT DISTINCT w FROM (
        SELECT split_part(summary, ' ', 1) AS w FROM (SELECT summary FROM vacancies WHERE status = 'published' ORDER BY random() LIMIT 400) x
        UNION ALL
        SELECT split_part(description, ' ', 1 + (random() * 100)::int) FROM (SELECT description FROM vacancies WHERE status = 'published' ORDER BY random() LIMIT 400) y
      ) z WHERE w <> ''
    ) q),
  'fields',    (SELECT json_agg(code) FROM (SELECT code FROM science_groups ORDER BY random() LIMIT 20) x),
  'regions',   (SELECT json_agg(code) FROM (SELECT code FROM regions ORDER BY random() LIMIT 20) x),
  'vacancies', (SELECT json_agg(id) FROM (SELECT id FROM vacancies WHERE status = 'published' ORDER BY random() LIMIT 2000) x),
  'profiles',  (SELECT json_agg(id) FROM (SELECT id FROM profiles WHERE visibility = 'public' AND headline <> '' ORDER BY random() LIMIT 2000) x),
  'orgs',      (SELECT json_agg(slug) FROM (SELECT slug FROM organizations ORDER BY random() LIMIT 300) x),
  'applicants', (SELECT json_agg(json_build_object('t', 'lt-a-' || n, 'e', email) ORDER BY n) FROM lt_applicants),
  'staff', (
    SELECT json_agg(json_build_object('t', 'lt-s-' || s.n, 'apps', coalesce(ap.ids, '[]'::json)) ORDER BY s.n)
    FROM lt_staff s
    LEFT JOIN LATERAL (
      SELECT json_agg(x.id) AS ids FROM (
        SELECT a.id FROM applications a JOIN vacancies v ON v.id = a.vacancy_id
        WHERE v.org_id = s.org_id AND a.status = 'sent'
        ORDER BY a.created_at DESC LIMIT 60
      ) x
    ) ap ON true),
  'apply', (
    SELECT json_agg(json_build_object('t', 'lt-a-' || a.n, 'v', o.id) ORDER BY a.n)
    FROM lt_applicants a
    JOIN lt_open o ON o.n = 1 + (a.n * 7919) % (SELECT max(n) FROM lt_open))
);
