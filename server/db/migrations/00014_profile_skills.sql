-- Навыки в профиле (D-136): научные (методы, приборы, программы для исследований) и общие (владение компьютером,
-- языки, работа с людьми — всё, что нужно не только в науке). Короткие фразы; число и длину проверяет internal/profiles.

-- +goose Up
ALTER TABLE profiles
    ADD COLUMN research_skills text[] NOT NULL DEFAULT '{}' CHECK (cardinality(research_skills) <= 30),
    ADD COLUMN general_skills  text[] NOT NULL DEFAULT '{}' CHECK (cardinality(general_skills) <= 30);

-- +goose Down
ALTER TABLE profiles DROP COLUMN general_skills, DROP COLUMN research_skills;
