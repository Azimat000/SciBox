-- Худший правдоподобный случай для break-ui. Только для базы scibox_e2e после scripts/e2e-db:
--   docker compose exec -T postgres psql -U scibox -d scibox_e2e < e2e/fixtures/worst-case.sql
UPDATE organizations SET name = 'Федеральное государственное бюджетное учреждение науки Институт проблем комплексного освоения недр имени академика Н. В. Мельникова Сибирского отделения Российской академии наук',
  city = 'Петропавловск-Камчатский'
 WHERE slug = 'tsentr-izucheniya-arktiki';
UPDATE units SET name = 'Лаборатория физико-химических методов исследования многокомпонентных систем и наноструктурированных материалов для водородной энергетики'
 WHERE org_id = (SELECT id FROM organizations WHERE slug = 'tsentr-izucheniya-arktiki');
UPDATE vacancies SET title = 'Ведущий научный сотрудник лаборатории физико-химических методов исследования многокомпонентных систем и наноструктурированных материалов для водородной энергетики (по конкурсу)',
  salary_from = 1500000, salary_to = 2400000, city = 'Петропавловск-Камчатский', deadline = (now() AT TIME ZONE 'Europe/Moscow')::date + 1
 WHERE title LIKE 'Ведущий научный сотрудник: климатические модели%';
UPDATE vacancies SET title = 'Лаборант' WHERE title = 'Менеджер научных проектов';
UPDATE users SET display_name = 'Анна-Мария Константиновна Римская-Корсакова-Голенищева' WHERE email = 'dmitry.korolev@demo.example.ru';
UPDATE users SET display_name = 'Ли Я' WHERE email = 'alina.guseva@demo.example.ru';
UPDATE profiles SET headline = 'Старший научный сотрудник лаборатории физико-химических методов исследования многокомпонентных систем, Институт проблем комплексного освоения недр имени академика Н. В. Мельникова СО РАН',
  contact_email = 'anna-maria.rimskaya-korsakova-golenishcheva@ipkon-sibirskoe-otdelenie-ran.example.ru',
  city = 'Петропавловск-Камчатский', h_scopus = 87, h_rsci = 112
 WHERE user_id = (SELECT id FROM users WHERE email = 'dmitry.korolev@demo.example.ru');
