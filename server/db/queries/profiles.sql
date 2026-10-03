-- Профили учёных (срез 7).

-- Профиль создаётся при первом обращении; повторный вызов ничего не меняет.
-- name: EnsureProfile :exec
INSERT INTO profiles (user_id, created_at, updated_at) VALUES (@user_id, @now, @now) ON CONFLICT (user_id) DO NOTHING;

-- Блокировка профиля на время правки: две одновременные правки одного профиля выстроятся в очередь.
-- name: LockProfileByUser :one
SELECT id FROM profiles WHERE user_id = $1 FOR UPDATE;

-- name: GetProfileByUser :one
SELECT sqlc.embed(p), u.display_name, r.name AS region_name, s.name AS degree_specialty_name
FROM profiles p
JOIN users u ON u.id = p.user_id
LEFT JOIN regions r ON r.code = p.region_code
LEFT JOIN specialties s ON s.code = p.degree_specialty_code
WHERE p.user_id = $1;

-- name: GetProfileByID :one
SELECT sqlc.embed(p), u.display_name, r.name AS region_name, s.name AS degree_specialty_name
FROM profiles p
JOIN users u ON u.id = p.user_id
LEFT JOIN regions r ON r.code = p.region_code
LEFT JOIN specialties s ON s.code = p.degree_specialty_code
WHERE p.id = $1;

-- name: UpdateProfileCore :execrows
UPDATE profiles SET
    headline = @headline, city = @city, region_code = @region_code, about = @about,
    degree = @degree, degree_specialty_code = @degree_specialty_code, degree_year = @degree_year,
    degree_institution = @degree_institution, dissertation_title = @dissertation_title,
    academic_title = @academic_title, academic_title_year = @academic_title_year,
    orcid = @orcid, spin = @spin, scopus_id = @scopus_id, wos_id = @wos_id,
    h_rsci = @h_rsci, h_scopus = @h_scopus, h_wos = @h_wos, h_scholar = @h_scholar,
    contact_email = @contact_email, updated_at = @now
WHERE id = @id;

-- name: UpdateProfilePrivacy :execrows
UPDATE profiles SET visibility = @visibility, open_to_offers = @open_to_offers, updated_at = @now WHERE id = @id;

-- name: TouchProfile :exec
UPDATE profiles SET updated_at = @now WHERE id = @id;

-- name: DeleteProfileSpecialties :exec
DELETE FROM profile_specialties WHERE profile_id = $1;

-- name: AddProfileSpecialties :exec
INSERT INTO profile_specialties (profile_id, specialty_code) SELECT @profile_id, unnest(@codes::text[]);

-- name: ListProfileSpecialties :many
SELECT s.code, s.name
FROM profile_specialties ps JOIN specialties s ON s.code = ps.specialty_code
WHERE ps.profile_id = $1
ORDER BY string_to_array(s.code, '.')::int[];

-- Записи разделов: от новых к старым.
-- name: ListProfileItems :many
SELECT id, kind, data, created_at, updated_at FROM profile_items
WHERE profile_id = $1
ORDER BY kind, sort_year DESC, created_at DESC;

-- name: CountProfileItems :one
SELECT count(*) FROM profile_items WHERE profile_id = @profile_id AND kind = @kind;

-- name: InsertProfileItem :one
INSERT INTO profile_items (profile_id, kind, sort_year, data, created_at, updated_at)
VALUES (@profile_id, @kind, @sort_year, @data, @now, @now) RETURNING id;

-- Запись вместе с хозяином: так проверяем, что человек правит свою.
-- name: GetProfileItem :one
SELECT i.id, i.profile_id, i.kind, p.user_id FROM profile_items i JOIN profiles p ON p.id = i.profile_id WHERE i.id = $1;

-- name: UpdateProfileItem :execrows
UPDATE profile_items SET sort_year = @sort_year, data = @data, updated_at = @now WHERE id = @id;

-- name: DeleteProfileItem :execrows
DELETE FROM profile_items WHERE id = $1;

-- Есть ли в профиле публикация с этим DOI (кроме записи except_id, которую как раз правят).
-- name: ProfileHasDOI :one
SELECT EXISTS (
    SELECT 1 FROM profile_items
    WHERE profile_id = @profile_id AND kind = 'publication' AND data ->> 'doi' = @doi::text
      AND id <> @except_id::uuid
);

-- Состоит ли человек хотя бы в одной организации (тогда ему виден профиль «организациям»).
-- name: IsOrgStaff :one
SELECT EXISTS (SELECT 1 FROM org_members WHERE user_id = $1);

-- Каталог учёных (срез 10). Показываются только профили в разрешённых режимах приватности (@modes решает пакет privacy)
-- и с заполненной должностью; сам смотрящий из каталога исключён. Слова ищутся по русской морфологии (имя, должность,
-- город, регион, организация степени, специальности, «о себе»); если точных совпадений нет, запрос повторяется «по похожим
-- словам» (@fuzzy, имя и должность).
-- name: SearchScientists :many
SELECT p.id, u.display_name, p.headline, p.city, r.name AS region_name, p.degree, p.academic_title, p.open_to_offers, p.updated_at,
       hh.h::int AS h_max,
       (SELECT count(*) FROM profile_items i WHERE i.profile_id = p.id AND i.kind = 'publication')::bigint AS publications,
       count(*) OVER () AS total
FROM profiles p
JOIN users u ON u.id = p.user_id
LEFT JOIN regions r ON r.code = p.region_code
LEFT JOIN LATERAL (
    SELECT string_agg(s.name, ' ') AS names
    FROM profile_specialties ps JOIN specialties s ON s.code = ps.specialty_code
    WHERE ps.profile_id = p.id
) sp ON true
LEFT JOIN LATERAL (
    SELECT GREATEST(COALESCE(p.h_rsci, 0), COALESCE(p.h_scopus, 0), COALESCE(p.h_wos, 0), COALESCE(p.h_scholar, 0)) AS h
) hh ON true
LEFT JOIN LATERAL (
    SELECT to_tsvector('russian', u.display_name || ' ' || p.headline || ' ' || p.city || ' ' || COALESCE(r.name, '') || ' '
                                  || p.degree_institution || ' ' || COALESCE(sp.names, '') || ' ' || p.about) AS doc
) d ON true
WHERE p.visibility = ANY(@modes::text[])
  AND p.headline <> ''
  AND (sqlc.narg(exclude_user)::uuid IS NULL OR p.user_id <> sqlc.narg(exclude_user)::uuid)
  AND (@q::text = '' OR CASE WHEN @fuzzy::bool
         THEN word_similarity(@q::text, u.display_name || ' ' || p.headline) >= 0.35
         ELSE d.doc @@ websearch_to_tsquery('russian', @q::text) END)
  AND (cardinality(@fields::text[]) = 0 OR EXISTS (
         SELECT 1 FROM profile_specialties ps, unnest(@fields::text[]) f
         WHERE ps.profile_id = p.id AND (ps.specialty_code = f OR ps.specialty_code LIKE f || '.%')))
  AND (@region::text = '' OR p.region_code = @region::text)
  AND (cardinality(@degrees::text[]) = 0 OR p.degree = ANY(@degrees::text[]))
  AND (cardinality(@titles::text[]) = 0 OR p.academic_title = ANY(@titles::text[]))
  AND (NOT @open_only::bool OR p.open_to_offers)
  AND (@h_min::int = 0 OR hh.h >= @h_min::int)
ORDER BY
  CASE WHEN @sort::text = 'relevance' AND @q::text <> '' THEN
    CASE WHEN @fuzzy::bool
      THEN word_similarity(@q::text, u.display_name || ' ' || p.headline)
      ELSE ts_rank_cd(d.doc, websearch_to_tsquery('russian', @q::text)) END
  END DESC NULLS LAST,
  CASE WHEN @sort::text = 'h_index' THEN hh.h END DESC NULLS LAST,
  CASE WHEN @sort::text = 'name' THEN u.display_name END ASC NULLS LAST,
  p.updated_at DESC, p.id
LIMIT @row_limit OFFSET @row_offset;

-- Специальности сразу у нескольких профилей (карточки каталога).
-- name: ListSpecialtiesForProfiles :many
SELECT ps.profile_id, s.code, s.name
FROM profile_specialties ps JOIN specialties s ON s.code = ps.specialty_code
WHERE ps.profile_id = ANY(@ids::uuid[])
ORDER BY ps.profile_id, string_to_array(s.code, '.')::int[];
