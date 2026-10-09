-- Генератор объёмных данных для замеров (срез 24, D-138, docs/SCALING.md «Цели»).
-- Запускается скриптом scripts/load-data на отдельной базе scibox_load: `psql -v scale=1 -v pwhash=... -f generate.sql`.
-- scale = 1 даёт объём «вся наука РФ»: 350 тыс. людей, 10 тыс. организаций, 300 тыс. вакансий, 1 млн откликов, 2 млн файлов
-- (только строки: самих файлов в хранилище нет), 5 млн публикаций. Все люди и организации вымышленные (почты @load.example.ru),
-- города и регионы настоящие. Данные детерминированы по номеру строки, кроме случайных выборов (random()).
--
-- Внешние ключи и триггеры на время загрузки выключены (session_replication_role = replica): строки согласованы по
-- построению, а текст для поиска вакансий и профилей пересобирается в конце одной командой.

\set ON_ERROR_STOP on
\timing off
SET synchronous_commit = off;
SET session_replication_role = replica;
SET maintenance_work_mem = '256MB';

SELECT (350000 * :scale)::int AS nu, greatest((10000 * :scale)::int, 10) AS no, (300000 * :scale)::int AS nv,
       (1000000 * :scale)::int AS na, (5000000 * :scale)::int AS npub, (1500000 * :scale)::int AS nn,
       (500000 * :scale)::int AS nfav, (100000 * :scale)::int AS nss, (100000 * :scale)::int AS nsess,
       (1000000 * :scale)::int AS naudit, (100000 * :scale)::int AS noff \gset

-- Словарь для текстов: 2500 разных слов (50 приставок × 50 корней), частота по закону, близкому к Ципфу (степень 2,5):
-- есть частые слова (десятки процентов текстов) и редкие (единицы). Словарь из 80 слов, как в первой версии,
-- делал каждое слово «найденным» почти везде и давал заведомо неверные замеры.
CREATE TEMP TABLE vocab AS
SELECT (row_number() OVER ())::int AS n, p || r AS w
FROM unnest(ARRAY['нейро','гео','био','микро','нано','термо','электро','фото','хемо','аэро','гидро','магнито','спектро','крио','эко','астро',
                  'хроно','био','сейсмо','радио','оптико','акусто','ферро','полу','сверх','квази','меж','транс','мета','пара','мульти',
                  'гипер','ультра','изо','стерео','акти','иммуно','онко','кардио','энерго','агро','лесо','океано','клима','мате','инфо','меха','робо','когни','нейтро']) WITH ORDINALITY AS a(p, i)
CROSS JOIN unnest(ARRAY['геном','белок','клетка','кристалл','спектр','плазма','нейтрон','лазер','полимер','катализ','синтез','мембрана',
                  'алгоритм','модель','граф','оптимизация','вычисление','симуляция','статистика','климат','океан','ледник','почва',
                  'атмосфера','экосистема','популяция','вирус','иммунитет','диагностика','сенсор','структура','проводник',
                  'топология','уравнение','поле','поток','механика','турбулентность','материал','сплав','керамика','композит','коррозия',
                  'накопитель','реактор','месторождение','картография','корпус','перевод']) WITH ORDINALITY AS b(r, j)
ORDER BY (a.i * 7 + b.j * 13) % 2500, a.i;
ALTER TABLE vocab ADD PRIMARY KEY (n);
ANALYZE vocab;

CREATE FUNCTION pg_temp.words(n int) RETURNS text LANGUAGE sql VOLATILE AS $$
  SELECT string_agg(v.w, ' ') FROM (SELECT 1 + floor((SELECT max(n) FROM vocab) * power(random(), 2.5))::int AS k FROM generate_series(1, n)) r JOIN vocab v ON v.n = r.k
$$;

CREATE FUNCTION pg_temp.pick(a text[]) RETURNS text LANGUAGE sql VOLATILE AS $$
  SELECT a[1 + floor(random() * array_length(a, 1))::int]
$$;

CREATE TEMP TABLE cfg AS SELECT
  ARRAY['Иванов','Петров','Сидоров','Смирнов','Кузнецов','Попов','Васильев','Соколов','Михайлов','Новиков','Фёдоров','Морозов',
        'Волков','Алексеев','Лебедев','Семёнов','Егоров','Павлов','Козлов','Степанов','Николаев','Орлов','Андреев','Макаров',
        'Никитин','Захаров','Зайцев','Соловьёв','Борисов','Яковлев','Григорьев','Романов','Воробьёв','Сергеев','Фролов','Александров',
        'Дмитриев','Королёв','Гусев','Киселёв']::text[] AS surnames,
  ARRAY['Александр','Дмитрий','Максим','Сергей','Андрей','Алексей','Артём','Илья','Кирилл','Михаил','Никита','Матвей','Роман',
        'Егор','Арсений','Иван','Денис','Евгений','Даниил','Тимофей','Анна','Мария','Елена','Ольга','Наталья','Татьяна','Ирина',
        'Екатерина','Светлана','Юлия','Анастасия','Дарья','Алина','Ксения','Вера','Надежда','Людмила','Галина','Виктория','Полина']::text[] AS names,
  ARRAY['Москва','Санкт-Петербург','Новосибирск','Екатеринбург','Казань','Нижний Новгород','Челябинск','Самара','Омск','Ростов-на-Дону',
        'Уфа','Красноярск','Воронеж','Пермь','Волгоград','Краснодар','Саратов','Тюмень','Тольятти','Ижевск','Барнаул','Ульяновск',
        'Иркутск','Хабаровск','Владивосток','Ярославль','Махачкала','Томск','Оренбург','Кемерово','Рязань','Астрахань','Пенза','Липецк',
        'Тула','Киров','Чебоксары','Калининград','Курск','Дубна']::text[] AS cities,
  ARRAY['Научный сотрудник','Старший научный сотрудник','Ведущий научный сотрудник','Заведующий лабораторией','Доцент','Профессор',
        'Аспирант','Младший научный сотрудник','Главный научный сотрудник','Инженер-исследователь']::text[] AS roles,
  ARRAY(SELECT code FROM positions ORDER BY code) AS positions,
  ARRAY(SELECT code FROM regions ORDER BY code) AS regions,
  ARRAY(SELECT code FROM specialties ORDER BY code) AS specialties;

-- Выбор журнала для публикации по номеру (массив из 53 тысяч строк на каждую из миллионов строк читался бы слишком долго).
CREATE TEMP TABLE issn_pool AS SELECT (row_number() OVER (ORDER BY issn))::int AS n, issn FROM journal_issns;
ALTER TABLE issn_pool ADD PRIMARY KEY (n);
ANALYZE issn_pool;

-- Люди.
INSERT INTO users (id, email, display_name, password_hash, email_confirmed_at, privacy_consent_at, privacy_policy_version, created_at, updated_at)
SELECT md5('u' || i)::uuid, 'p' || i || '@load.example.ru',
       c.surnames[1 + i % 40] || ' ' || c.names[1 + (i / 40) % 40],
       :'pwhash', now() - interval '1 day' * (random() * 900), now() - interval '1 day' * (random() * 900), '2026-10-draft',
       now() - interval '1 day' * (random() * 900), now() - interval '1 day' * (random() * 30)
FROM generate_series(1, :nu) i, cfg c;

-- Организации, подразделения, сотрудники.
INSERT INTO organizations (id, slug, name, kind, city, website, description, created_by, created_at, updated_at)
SELECT md5('o' || i)::uuid, 'org-' || i,
       (ARRAY['Университет','Институт','Научный центр','Лаборатория','Технопарк','Академия'])[1 + i % 6] || ' ' || c.cities[1 + i % 40] || ' №' || i,
       (ARRAY['university','institute','science_center','rd_company','technopark','other'])[1 + i % 6], c.cities[1 + i % 40],
       'https://org' || i || '.example.ru', pg_temp.words(25), md5('u' || i)::uuid, now() - interval '1 day' * (random() * 900), now()
FROM generate_series(1, :no) i, cfg c;

INSERT INTO units (id, org_id, name, kind, description, topics, head_user_id, created_at, updated_at)
SELECT md5('n' || j)::uuid, md5('o' || (1 + (j - 1) % :no))::uuid,
       (ARRAY['Кафедра','Лаборатория','Отдел','Центр коллективного пользования'])[1 + j % 4] || ' ' || pg_temp.words(2) || ' №' || j,
       (ARRAY['department','laboratory','division','shared_facility'])[1 + j % 4], pg_temp.words(15), ARRAY[pg_temp.words(1), pg_temp.words(1)],
       CASE WHEN j <= :no THEN md5('u' || (2 * :no + j))::uuid END, now() - interval '1 day' * (random() * 900), now()
FROM generate_series(1, 3 * :no) j;

INSERT INTO org_members (org_id, user_id, role, joined_at)
SELECT md5('o' || i)::uuid, md5('u' || (i + k * :no))::uuid, (ARRAY['owner','hr','unit_head'])[1 + k], now() - interval '1 day' * (random() * 600)
FROM generate_series(1, :no) i, generate_series(0, 2) k;

-- Вакансии. На 100 вакансий: 5 черновиков, 10 опубликованных, 20 закрытых, 65 архивных. У организации вакансий неравномерно
-- (квадрат случайного числа): есть большие организации-«лидеры».
INSERT INTO vacancies (id, org_id, unit_id, created_by, status, title, position_code, summary, description, requirements, focus,
                       career_level, work_format, region_code, city, housing, rate_percent, salary_from, salary_to, contract_type,
                       contract_months, funding_source, degree_required, title_required, is_competition, deadline,
                       published_at, closed_at, archived_at, created_at, updated_at)
SELECT md5('v' || i)::uuid, md5('o' || org.o)::uuid,
       CASE WHEN i % 5 <> 0 THEN md5('n' || (org.o + :no * (i % 3)))::uuid END, md5('u' || (i % :no + 1))::uuid,
       s.status,
       c.roles[1 + i % 10] || ' ' || pg_temp.words(2) || ' (' || c.cities[1 + i % 40] || ')',
       c.positions[1 + i % array_length(c.positions, 1)], pg_temp.words(25), pg_temp.words(150), pg_temp.words(60), pg_temp.words(4),
       1 + i % 4, (ARRAY['onsite','onsite','hybrid','remote'])[1 + i % 4], c.regions[1 + (i / 3) % array_length(c.regions, 1)],
       c.cities[1 + (i / 5) % 40], (ARRAY['none','none','none','dormitory','service','compensation'])[1 + i % 6],
       (ARRAY[25,50,75,100,100,100])[1 + i % 6],
       CASE WHEN i % 5 < 3 THEN 40000 + (i % 40) * 5000 END, CASE WHEN i % 5 < 3 THEN 60000 + (i % 40) * 6000 END,
       CASE WHEN i % 4 = 0 THEN 'fixed' ELSE 'permanent' END, CASE WHEN i % 4 = 0 THEN 6 + (i % 30) END,
       (ARRAY['budget','grant','contract','own'])[1 + i % 4], (ARRAY['none','none','candidate','doctor'])[1 + i % 4],
       (ARRAY['none','none','none','docent','professor'])[1 + i % 5], i % 9 = 0,
       CASE WHEN s.status = 'published' THEN
              CASE WHEN i % 7 = 0 THEN NULL WHEN i % 7 = 1 THEN current_date - 3 - (i % 20) ELSE current_date + 3 + (i % 120) END
            ELSE current_date - (i % 400) END,
       CASE WHEN s.status <> 'draft' THEN now() - interval '1 day' * (random() * 400) END,
       CASE WHEN s.status IN ('closed', 'archived') THEN now() - interval '1 day' * (random() * 200) END,
       CASE WHEN s.status = 'archived' THEN now() - interval '1 day' * (random() * 100) END,
       now() - interval '1 day' * (400 + random() * 500), now() - interval '1 day' * (random() * 300)
FROM generate_series(1, :nv) i
CROSS JOIN cfg c
CROSS JOIN LATERAL (SELECT 1 + floor(:no * power(random(), 2))::int + 0 * i) org(o)
CROSS JOIN LATERAL (SELECT CASE WHEN i % 20 = 0 THEN 'draft' WHEN i % 20 IN (1, 2) THEN 'published'
                                WHEN i % 20 BETWEEN 3 AND 6 THEN 'closed' ELSE 'archived' END AS status) s;

INSERT INTO vacancy_specialties (vacancy_id, specialty_code)
SELECT md5('v' || i)::uuid, c.specialties[1 + floor(random() * array_length(c.specialties, 1))::int + 0 * k]
FROM generate_series(1, :nv) i CROSS JOIN cfg c CROSS JOIN LATERAL generate_series(1, 1 + i % 3) k
ON CONFLICT DO NOTHING;

-- Профили: у каждого человека один. 35% скрыты, 25% видны организациям, 40% публичные; у 10% должность не заполнена.
INSERT INTO profiles (id, user_id, visibility, open_to_offers, headline, city, region_code, about, degree, degree_specialty_code,
                      degree_year, degree_institution, dissertation_title, academic_title, academic_title_year, h_rsci, h_scopus, h_wos,
                      h_scholar, research_skills, general_skills, created_at, updated_at)
SELECT md5('p' || i)::uuid, md5('u' || i)::uuid,
       CASE WHEN i % 20 < 7 THEN 'hidden' WHEN i % 20 < 12 THEN 'orgs' ELSE 'public' END, i % 5 = 0,
       CASE WHEN i % 10 = 9 THEN '' ELSE c.roles[1 + i % 10] || ', ' || pg_temp.words(2) END,
       c.cities[1 + (i / 7) % 40], c.regions[1 + (i / 11) % array_length(c.regions, 1)], pg_temp.words(45),
       (ARRAY['none','candidate','candidate','doctor'])[1 + i % 4], CASE WHEN i % 4 > 0 THEN c.specialties[1 + i % 350] ELSE NULL END,
       CASE WHEN i % 4 > 0 THEN 1995 + i % 30 END, CASE WHEN i % 4 > 0 THEN 'Университет ' || c.cities[1 + (i / 3) % 40] ELSE '' END,
       CASE WHEN i % 4 > 0 THEN pg_temp.words(6) ELSE '' END, (ARRAY['none','none','docent','professor'])[1 + i % 4],
       CASE WHEN i % 4 > 1 THEN 2000 + i % 25 END, i % 61, i % 53, i % 47, i % 67,
       ARRAY[pg_temp.words(1), pg_temp.words(1), pg_temp.words(1)], ARRAY[pg_temp.words(1), pg_temp.words(1)],
       now() - interval '1 day' * (random() * 900), now() - interval '1 day' * (random() * 700)
FROM generate_series(1, :nu) i CROSS JOIN cfg c;

INSERT INTO profile_specialties (profile_id, specialty_code)
SELECT md5('p' || i)::uuid, c.specialties[1 + floor(random() * array_length(c.specialties, 1))::int + 0 * k]
FROM generate_series(1, :nu) i CROSS JOIN cfg c CROSS JOIN LATERAL generate_series(1, 1 + i % 3) k
ON CONFLICT DO NOTHING;

-- Публикации: в среднем 14 на человека, журналы настоящие (из справочника SCImago), поэтому доля Q1–Q2 такая же, как у журналов.
INSERT INTO profile_items (id, profile_id, kind, sort_year, data, created_at, updated_at)
SELECT md5('i' || k)::uuid, md5('p' || (1 + k % :nu))::uuid, 'publication', y.y::smallint,
       jsonb_build_object('doi', '10.5555/load.' || k, 'issn', (SELECT p.issn FROM issn_pool p WHERE p.n = y.r),
                          'year', y.y, 'issue', (1 + k % 12)::text, 'pages', (100 + k % 800) || '–' || (110 + k % 800),
                          'title', pg_temp.words(7), 'venue', 'Журнал ' || pg_temp.words(2), 'source', 'manual',
                          'volume', (1 + k % 90)::text, 'authors', 'Иванов И. И., Петров П. П., Сидоров С. С.', 'pub_type', 'article'),
       now() - interval '1 day' * (random() * 700), now() - interval '1 day' * (random() * 300)
FROM generate_series(1, :npub) k CROSS JOIN cfg c
CROSS JOIN LATERAL (SELECT 2026 - floor(22 * power(random(), 1.8))::int + 0 * k AS y,
                           1 + floor(random() * (SELECT count(*) FROM issn_pool))::int + 0 * k AS r) y;

-- Образование (по одной записи) и опыт (по две).
INSERT INTO profile_items (id, profile_id, kind, sort_year, data, created_at, updated_at)
SELECT md5('e' || i || ':' || k)::uuid, md5('p' || i)::uuid, CASE WHEN k = 0 THEN 'education' ELSE 'experience' END, (2000 + (i + k) % 25)::smallint,
       CASE WHEN k = 0 THEN jsonb_build_object('program', 'Аспирантура', 'year_to', 2000 + i % 25, 'year_from', 1996 + i % 25, 'institution', 'Университет ' || c.cities[1 + i % 40])
            ELSE jsonb_build_object('role', c.roles[1 + (i + k) % 10], 'year_to', 2005 + (i + k) % 20, 'year_from', 2000 + (i + k) % 20, 'organization', 'Институт ' || c.cities[1 + (i + k) % 40]) END,
       now(), now()
FROM generate_series(1, :nu) i CROSS JOIN cfg c CROSS JOIN generate_series(0, 2) k;

-- Отклики. Вакансии выбираются неравномерно (квадрат случайного числа: лидеры собирают больше). Повторная пара «человек — вакансия»
-- отбрасывается уникальным индексом, поэтому откликов чуть меньше заказанного.
CREATE TEMP TABLE snapshot AS
SELECT jsonb_build_object('name', 'Кандидат', 'about', pg_temp.words(60), 'headline', 'Научный сотрудник, лаборатория',
         'h_index', jsonb_build_object('wos', 5, 'rsci', 6, 'scopus', 7, 'scholar', 9),
         'sections', jsonb_build_object(
           'publications', (SELECT jsonb_agg(jsonb_build_object('kind', 'publication', 'title', pg_temp.words(8), 'year', 2015 + g % 11,
                              'venue', 'Журнал ' || pg_temp.words(2), 'authors', 'Иванов И. И., Петров П. П.', 'doi', '10.5555/x.' || g,
                              'issn', '1063-7834', 'pages', '412–425', 'volume', '66', 'issue', '3')) FROM generate_series(1, 9) g),
           'education', jsonb_build_array(jsonb_build_object('kind', 'education', 'program', 'Аспирантура', 'institution', 'Университет')),
           'grants', '[]'::jsonb, 'patents', '[]'::jsonb, 'teaching', '[]'::jsonb)) AS doc;

INSERT INTO applications (id, vacancy_id, user_id, status, cover_letter, contact_email, profile, created_at, updated_at, status_changed_at)
SELECT md5('a' || i)::uuid, md5('v' || v.v)::uuid, md5('u' || u.u)::uuid,
       (ARRAY['sent','sent','sent','sent','viewed','viewed','viewed','invited','rejected','rejected','accepted','withdrawn'])[1 + i % 12],
       pg_temp.words(50), 'p' || u.u || '@load.example.ru', t.doc,
       now() - interval '1 day' * (random() * 400), now() - interval '1 day' * (random() * 300), now() - interval '1 day' * (random() * 300)
FROM generate_series(1, :na) i CROSS JOIN snapshot t
CROSS JOIN LATERAL (SELECT 1 + floor(:nv * power(random(), 1.5))::int + 0 * i) v(v)
CROSS JOIN LATERAL (SELECT 1 + floor(:nu * random())::int + 0 * i) u(u)
ON CONFLICT DO NOTHING;

-- Файлы откликов: резюме и ещё 0–2 приложения у каждого отклика (в среднем 2). Строки есть, объектов в хранилище нет.
INSERT INTO application_files (id, application_id, kind, name, size, position, created_at, storage_key, sha256, scan_status, scanned_at, scan_attempts)
SELECT md5('f' || a.id || k)::uuid, a.id, CASE WHEN k = 0 THEN 'cv' ELSE 'attachment' END,
       CASE WHEN k = 0 THEN 'Резюме.pdf' ELSE 'Приложение ' || k || '.pdf' END, 50000 + (get_byte(decode(md5(a.id::text || k), 'hex'), 1) * 11000),
       k::smallint, a.created_at, 'files/' || md5('f' || a.id || k)::uuid, sha256(convert_to(md5('f' || a.id || k), 'UTF8')), 'clean', a.created_at, 0
FROM applications a
CROSS JOIN LATERAL generate_series(0, get_byte(decode(md5(a.id::text), 'hex'), 0) % 3) k;

-- Уведомления (неравномерно: у активных людей больше), избранное, сохранённые поиски, сеансы, журнал действий, приглашения.
INSERT INTO notifications (id, user_id, kind, title, body, link, created_at, read_at)
SELECT md5('nt' || i)::uuid, md5('u' || (1 + floor(:nu * power(random(), 1.4))::int + 0 * i))::uuid,
       (ARRAY['application_status','new_vacancies','offer','deadline'])[1 + i % 4], 'Новое уведомление ' || i, pg_temp.words(10), '/applications',
       now() - interval '1 day' * (random() * 400), CASE WHEN i % 5 <> 0 THEN now() - interval '1 day' * (random() * 300) END
FROM generate_series(1, :nn) i;

INSERT INTO favorites (user_id, vacancy_id, created_at)
SELECT md5('u' || (1 + floor(:nu * random())::int + 0 * i))::uuid, md5('v' || (1 + floor(:nv * power(random(), 1.5))::int))::uuid, now() - interval '1 day' * (random() * 300)
FROM generate_series(1, :nfav) i
ON CONFLICT DO NOTHING;

INSERT INTO saved_searches (id, user_id, name, query, frequency, checked_at, next_run_at, created_at, updated_at)
SELECT md5('ss' || i)::uuid, md5('u' || (1 + floor(:nu * random())::int + 0 * i))::uuid, 'Поиск ' || i, 'q=' || pg_temp.words(1) || '&region=' || (1 + i % 89),
       (ARRAY['instant','daily','weekly','off'])[1 + i % 4], now() - interval '1 day', now() + interval '1 hour' * (random() * 24), now(), now()
FROM generate_series(1, :nss) i;

INSERT INTO sessions (id, user_id, token_hash, created_at, last_seen_at, expires_at, user_agent, ip)
SELECT md5('s' || i)::uuid, md5('u' || (1 + floor(:nu * random())::int + 0 * i))::uuid, sha256(convert_to('token' || i, 'UTF8')),
       now() - interval '1 day' * (random() * 25), now() - interval '1 hour' * (random() * 48), now() + interval '1 day' * (1 + random() * 29),
       'Mozilla/5.0 (X11; Linux x86_64) Chrome/130', '10.' || (i % 250) || '.' || (i / 250 % 250) || '.1'
FROM generate_series(1, :nsess) i;

INSERT INTO audit_log (at, org_id, actor_id, subject_id, action, target_kind, target_id, target_label, details)
SELECT now() - interval '1 day' * (random() * 1000), md5('o' || (1 + floor(:no * power(random(), 2))::int + 0 * i))::uuid,
       md5('u' || (1 + i % :nu))::uuid, NULL, (ARRAY['vacancy.published','vacancy.closed','application.viewed','application.decided','file.downloaded'])[1 + i % 5],
       'application', md5('a' || (1 + i % :na))::uuid, 'Отклик ' || i, '{}'::jsonb
FROM generate_series(1, :naudit) i;

INSERT INTO vacancy_offers (id, vacancy_id, user_id, invited_by, status, message, answer_note, answered_at, created_at, updated_at)
SELECT md5('of' || i)::uuid, md5('v' || (1 + floor(:nv * power(random(), 1.5))::int + 0 * i))::uuid, md5('u' || (1 + floor(:nu * random())::int))::uuid,
       md5('u' || (1 + i % :no))::uuid, s.st, 'Приглашаем вас на вакансию', CASE WHEN s.st IN ('interested', 'declined') THEN 'Спасибо' ELSE '' END,
       CASE WHEN s.st IN ('interested', 'declined') THEN now() ELSE NULL END, now() - interval '1 day' * (random() * 200), now()
FROM generate_series(1, :noff) i CROSS JOIN LATERAL (SELECT (ARRAY['pending','interested','declined','cancelled'])[1 + i % 4] AS st) s
ON CONFLICT DO NOTHING;

RESET session_replication_role;

-- Тексты поиска вакансий пересобираются функцией самой базы (триггеры на время загрузки были выключены).
SELECT refresh_vacancy_search(id) FROM vacancies \g /dev/null

ANALYZE;
