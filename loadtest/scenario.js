// Нагрузочный сценарий SciBox (срез 27, D-177). Запускает scripts/load-test; вручную:
//   k6 run -e BASES=http://127.0.0.1:8091,http://127.0.0.1:8092 -e FIXTURES=loadtest/.fixtures.json \
//          -e RPS=300 -e DURATION=60s -e PASSWORD=... loadtest/scenario.js
//
// Модель открытая: запросы приходят с заданной скоростью, как бы медленно ни отвечал сервер (так сервер, который не
// успевает, виден как рост задержек и «потерянные» запросы, а не как тихое замедление самого теста). Копии сервера
// чередуются по кругу. «Посетители» разные: каждый запрос несёт адрес из пула в X-Forwarded-For (сервер доверяет
// этому прокси), поэтому настоящее ограничение частоты работает и его запись в базу входит в замер.
import http from 'k6/http';
import exec from 'k6/execution';
import { SharedArray } from 'k6/data';
import { Counter, Rate } from 'k6/metrics';

const BASES = (__ENV.BASES || 'http://127.0.0.1:8080').split(',');
const FIXTURES = __ENV.FIXTURES || 'loadtest/.fixtures.json';
const RPS = Number(__ENV.RPS || 100);
const DURATION = __ENV.DURATION || '30s';
const PASSWORD = __ENV.PASSWORD || '';
const PAIR_OFFSET = Number(__ENV.PAIR_OFFSET || 0);
const SUMMARY_OUT = __ENV.SUMMARY_OUT || 'loadtest/.summary.json';

const raw = (key) => new SharedArray(key, () => JSON.parse(open(FIXTURES))[key] || []);
const words = raw('words');
const fields = raw('fields');
const regions = raw('regions');
const vacancies = raw('vacancies');
const profiles = raw('profiles');
const orgs = raw('orgs');
const applicants = raw('applicants');
const staff = raw('staff');
const applyPairs = raw('apply');

const POSITION_TYPES = ['research', 'teaching', 'admin', 'phd', 'masters', 'project', 'internship'];
const CITIES = ['Москва', 'Казань', 'Новосибирск', 'Томск', 'Самара', 'Пермь', 'Дубна', 'Тула'];

// Пул адресов посетителей: 20 тысяч разных, у каждого около 0,02 запроса в секунду при 500 в секунду.
const IPS = [];
for (let i = 0; i < 20000; i++) {
  IPS.push(`${11 + (i % 200)}.${(i * 7) % 256}.${(i * 13) % 256}.${1 + (i * 17) % 254}`);
}

// Минимальный PDF (метка в начале файла, остальное заполнитель): сервер проверяет вид файла по содержимому.
const pdfBytes = (() => {
  const head = '%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\ntrailer<</Root 1 0 R>>\n';
  const pad = 'x'.repeat(100 * 1024);
  return head + '% ' + pad + '\n%%EOF\n';
})();

const server5xx = new Rate('server_errors'); // доля ответов 5xx среди всех запросов
const throttled = new Counter('throttled'); // 429
const netfail = new Counter('network_failures'); // нет ответа (срок, обрыв)
const unexpected = new Counter('unexpected_status'); // не тот код, который ждали

function pick(arr, i) {
  return arr[i % arr.length];
}
// Номер отправки сообщения: случайный UUID версии 4 (в k6 нет встроенного).
function uuidv4() {
  const h = '0123456789abcdef';
  let out = '';
  for (let i = 0; i < 36; i++) {
    if (i === 8 || i === 13 || i === 18 || i === 23) out += '-';
    else if (i === 14) out += '4';
    else if (i === 19) out += h[8 + rnd(4)];
    else out += h[rnd(16)];
  }
  return out;
}
function rnd(n) {
  return Math.floor(Math.random() * n);
}
function word() {
  return pick(words, rnd(1e9));
}
function typo() {
  const w = Array.from(pick(words, rnd(1e9)));
  w[Math.floor(w.length / 2)] = 'ъ';
  return w.join('');
}
const q = encodeURIComponent;

// Описание вида запроса: weight — доля в смеси (на тысячу), who — кем (guest, applicant, staff), budget — p95 в мс
// из docs/SCALING.md (чтение 200, запись 400), req() возвращает [метод, путь, тело, ожидаемый код].
const KINDS = [
  // Гость: поиск и страницы.
  { name: 'vacancies.search', weight: 50, who: 'guest', budget: 200, req: () => ['GET', '/api/vacancies?limit=20&offset=' + 20 * rnd(40)] },
  { name: 'vacancies.search.word', weight: 100, who: 'guest', budget: 200, req: () => ['GET', '/api/vacancies?q=' + q(word())] },
  { name: 'vacancies.search.filters', weight: 70, who: 'guest', budget: 200, req: () => ['GET', `/api/vacancies?field=${pick(fields, rnd(1e9))}&region=${pick(regions, rnd(1e9))}&type=${pick(POSITION_TYPES, rnd(1e9))}&sort=deadline`] },
  { name: 'vacancies.search.fuzzy', weight: 10, who: 'guest', budget: 200, req: () => ['GET', '/api/vacancies?q=' + q(typo())] },
  { name: 'vacancy.page', weight: 140, who: 'guest', budget: 200, req: () => ['GET', '/api/vacancies/' + pick(vacancies, rnd(1e9))] },
  { name: 'vacancies.of.org', weight: 20, who: 'guest', budget: 200, req: () => ['GET', '/api/vacancies?org=' + pick(orgs, rnd(1e9))] },
  { name: 'landing', weight: 40, who: 'guest', budget: 200, req: () => ['GET', '/api/landing'] },
  { name: 'reference', weight: 30, who: 'guest', budget: 200, req: () => ['GET', '/api/reference'] },
  { name: 'organizations.search', weight: 10, who: 'guest', budget: 200, req: () => ['GET', '/api/organizations?q=' + q(pick(CITIES, rnd(8)))] },
  { name: 'organization.page', weight: 20, who: 'guest', budget: 200, req: () => ['GET', '/api/organizations/' + pick(orgs, rnd(1e9))] },
  { name: 'scientists.search', weight: 20, who: 'guest', budget: 200, req: () => ['GET', '/api/scientists?limit=20&offset=' + 20 * rnd(40)] },
  { name: 'scientists.search.word', weight: 20, who: 'guest', budget: 200, req: () => ['GET', '/api/scientists?q=' + q(word())] },
  { name: 'scientist.page', weight: 30, who: 'guest', budget: 200, req: () => ['GET', '/api/scientists/' + pick(profiles, rnd(1e9))] },

  // Вошедший учёный: то, что сайт спрашивает на каждой странице, и личные списки.
  { name: 'me', weight: 90, who: 'applicant', budget: 200, req: () => ['GET', '/api/auth/me'] },
  { name: 'notifications.unread', weight: 70, who: 'applicant', budget: 200, req: () => ['GET', '/api/notifications/unread-count'] },
  { name: 'notifications.list', weight: 20, who: 'applicant', budget: 200, req: () => ['GET', '/api/notifications?limit=10'] },
  { name: 'applications.mine', weight: 30, who: 'applicant', budget: 200, req: () => ['GET', '/api/applications'] },
  { name: 'matches', weight: 30, who: 'applicant', budget: 200, req: () => ['GET', '/api/matches'] },
  { name: 'favorites', weight: 10, who: 'applicant', budget: 200, req: () => ['GET', '/api/favorites'] },
  { name: 'deadlines', weight: 10, who: 'applicant', budget: 200, req: () => ['GET', '/api/deadlines'] },
  { name: 'profile.own', weight: 20, who: 'applicant', budget: 200, req: () => ['GET', '/api/profile'] },
  { name: 'vacancy.page.signed', weight: 30, who: 'applicant', budget: 200, req: () => ['GET', '/api/vacancies/' + pick(vacancies, rnd(1e9))] },
  { name: 'vacancy.state', weight: 20, who: 'applicant', budget: 200, req: () => ['GET', '/api/applications/for-vacancy/' + pick(vacancies, rnd(1e9))] },
  { name: 'vacancies.search.signed', weight: 20, who: 'applicant', budget: 200, req: () => ['GET', '/api/vacancies?q=' + q(word())] },

  // Сотрудник организации.
  { name: 'staff.candidates', weight: 20, who: 'staff', budget: 200, req: () => ['GET', '/api/my/candidates'] },
  { name: 'staff.candidates.page', weight: 10, who: 'staff', budget: 200, req: () => ['GET', '/api/my/candidates?status=sent&offset=' + 20 * rnd(10)] },
  { name: 'staff.vacancies', weight: 5, who: 'staff', budget: 200, req: () => ['GET', '/api/my/vacancies'] },
  { name: 'staff.scientists', weight: 10, who: 'staff', budget: 200, req: () => ['GET', '/api/scientists?q=' + q(word()) + '&open=1'] },

  // Чат (срез 39): значок в шапке и список читаются часто, окно переписки открывает шапку и ленту, запись идёт редко.
  { name: 'chats.unread', weight: 30, who: 'applicant', budget: 200, req: () => ['GET', '/api/chats/unread-count'] },
  { name: 'chats.list', weight: 15, who: 'applicant', budget: 200, req: () => ['GET', '/api/chats?side=scientist&limit=30'] },
  { name: 'staff.chats.unread', weight: 10, who: 'staff', budget: 200, req: () => ['GET', '/api/chats/unread-count'] },
  { name: 'staff.chats.list', weight: 8, who: 'staff', budget: 200, req: () => ['GET', '/api/chats?side=org&limit=30'] },
  { name: 'chat.header', weight: 10, who: 'applicant', budget: 200, special: 'chat-header' },
  { name: 'chat.messages', weight: 10, who: 'applicant', budget: 200, special: 'chat-messages' },
  { name: 'staff.chat.messages', weight: 5, who: 'staff', budget: 200, special: 'chat-messages' },

  // Запись (бюджет 400): вход, избранное, решение по отклику, отклик без файла.
  { name: 'login', weight: 10, who: 'applicant', budget: 400, special: 'login' },
  { name: 'favorite.add', weight: 5, who: 'applicant', budget: 400, req: () => ['PUT', '/api/favorites/' + pick(vacancies, rnd(1e9)), null, [200, 204]] },
  { name: 'staff.decide', weight: 5, who: 'staff', budget: 400, special: 'decide' },
  // Чат: отправка и отметка «прочитано» (срез 39). Закрытая переписка отвечает 409, это ожидаемо.
  { name: 'chat.send', weight: 4, who: 'applicant', budget: 400, special: 'chat-send' },
  { name: 'staff.chat.send', weight: 2, who: 'staff', budget: 400, special: 'chat-send' },
  { name: 'chat.read', weight: 4, who: 'applicant', budget: 400, special: 'chat-read' },
  { name: 'apply', weight: 10, who: 'applicant', budget: 400, special: 'apply' },
  // Отклик с файлом: проходит хранилище и антивирус, бюджет шире (D-159: файлы вне бюджета записи).
  { name: 'apply.file', weight: 3, who: 'applicant', budget: 1500, special: 'apply-file' },
];

const total = KINDS.reduce((s, k) => s + k.weight, 0);
const cumulative = [];
KINDS.reduce((s, k) => {
  cumulative.push(s + k.weight);
  return s + k.weight;
}, 0);

function chooseKind() {
  const r = Math.random() * total;
  for (let i = 0; i < cumulative.length; i++) {
    if (r < cumulative[i]) return KINDS[i];
  }
  return KINDS[KINDS.length - 1];
}

// Пороги описывают бюджеты и заодно заставляют k6 вести счётчики по виду запроса (без порога строка в итог не попадает).
const thresholds = {
  server_errors: ['rate<0.001'], // < 0,1% ответов 5xx (docs/SCALING.md)
  dropped_iterations: ['count==0'], // сервер не успевает: часть запросов не отправлена вовсе
  throttled: ['count==0'],
  network_failures: ['count==0'],
  unexpected_status: ['count==0'],
};
for (const k of KINDS) {
  thresholds[`http_req_duration{name:${k.name}}`] = [`p(95)<${k.budget}`, 'p(99)<1000'];
  thresholds[`http_reqs{name:${k.name}}`] = ['count>=0'];
}

export const options = {
  scenarios: {
    mixed: {
      executor: 'constant-arrival-rate',
      rate: RPS,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: Math.max(50, Math.ceil(RPS / 2)),
      maxVUs: Math.max(500, RPS * 4),
    },
  },
  thresholds,
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  discardResponseBodies: false,
  noConnectionReuse: false,
  userAgent: 'scibox-loadtest',
};

// Куки руками (строкой): общая «банка» k6 в этих запросах не участвует, иначе вход одного человека сделал бы всех
// следующих запросов того же потока вошедшими.
const emptyJar = new http.CookieJar();

function session(kind, i) {
  if (kind.who === 'applicant') return pick(applicants, i).t;
  if (kind.who === 'staff') return pick(staff, i).t;
  return '';
}

function send(kind, method, path, body, token, expect, extra) {
  const base = BASES[exec.scenario.iterationInTest % BASES.length];
  const headers = Object.assign({ 'X-Forwarded-For': IPS[rnd(IPS.length)], Accept: 'application/json' }, extra || {});
  if (token) headers.Cookie = 'scibox_session=' + token;
  if (typeof body === 'string') headers['Content-Type'] = 'application/json';
  const res = http.request(method, base + path, body, {
    headers,
    jar: kind.special === 'login' ? new http.CookieJar() : emptyJar,
    timeout: '10s',
    tags: { name: kind.name },
    responseCallback: http.expectedStatuses(...expect),
  });
  const codes = expect;
  server5xx.add(res.status >= 500, { name: kind.name });
  if (res.status === 0) netfail.add(1, { name: kind.name });
  else if (res.status === 429) throttled.add(1, { name: kind.name });
  else if (res.status < 500 && !codes.includes(res.status)) unexpected.add(1, { name: kind.name });
  return res;
}

export default function () {
  const kind = chooseKind();
  const i = exec.scenario.iterationInTest;
  switch (kind.special) {
    case 'login': {
      const a = pick(applicants, rnd(1e9));
      send(kind, 'POST', '/api/auth/login', JSON.stringify({ email: a.e, password: PASSWORD }), '', [200]);
      return;
    }
    case 'chat-header':
    case 'chat-messages':
    case 'chat-send':
    case 'chat-read': {
      const who = kind.who === 'staff' ? pick(staff, rnd(1e9)) : pick(applicants, rnd(1e9));
      const ids = kind.who === 'staff' ? who.chats : who.c;
      if (!ids || !ids.length) return;
      const chat = pick(ids, rnd(1e9));
      if (kind.special === 'chat-header') send(kind, 'GET', `/api/chats/${chat}`, null, who.t, [200, 404]);
      else if (kind.special === 'chat-messages') send(kind, 'GET', `/api/chats/${chat}/messages?limit=30`, null, who.t, [200, 404]);
      else if (kind.special === 'chat-read') send(kind, 'POST', `/api/chats/${chat}/read`, JSON.stringify({ up_to: 1000000 }), who.t, [204, 404]);
      else send(kind, 'POST', `/api/chats/${chat}/messages`, JSON.stringify({ text: 'Нагрузка: ' + i, client_id: uuidv4() }), who.t, [200, 201, 404, 409]);
      return;
    }
    case 'decide': {
      const s = pick(staff, rnd(1e9));
      if (!s.apps.length) return;
      // Каждое решение по своему отклику один раз: номер отклика берётся из общего счётчика сценария.
      const app = s.apps[(PAIR_OFFSET + i) % s.apps.length];
      send(kind, 'POST', `/api/applications/${app}/status`, JSON.stringify({ status: 'rejected', note: '' }), s.t, [204, 404, 409, 422]);
      return;
    }
    case 'apply':
    case 'apply-file': {
      const pair = applyPairs[(PAIR_OFFSET + i) % applyPairs.length];
      const data = JSON.stringify({
        vacancy_id: pair.v,
        contact_email: 'lt' + i + '@load.example.ru',
        cover_letter: 'Нагрузочное письмо: интересна ваша работа, опыт в смежной области, готов обсудить детали и сроки.',
        referees: [],
      });
      const parts = { data: http.file(data, 'data', 'application/json') };
      if (kind.special === 'apply-file') parts.file = http.file(pdfBytes, 'letter.pdf', 'application/pdf');
      send(kind, 'POST', '/api/applications', parts, pair.t, [201, 409], {});
      return;
    }
    default: {
      const [method, path, body, ok] = kind.req();
      send(kind, method, path, body || null, session(kind, rnd(1e9)), ok || [200]);
    }
  }
}

// Итог: таблица по видам запросов и JSON для сравнения прогонов.
export function handleSummary(data) {
  const rows = [];
  const m = data.metrics;
  for (const k of KINDS) {
    const d = m[`http_req_duration{name:${k.name}}`];
    const c = m[`http_reqs{name:${k.name}}`];
    if (!d || !c) continue;
    const v = d.values;
    rows.push({
      name: k.name, count: c.values.count, rps: c.values.rate,
      avg: v.avg, p50: v.med, p95: v['p(95)'], p99: v['p(99)'], max: v.max, budget: k.budget,
      ok: v['p(95)'] < k.budget && v['p(99)'] < 1000,
    });
  }
  const g = (n, f) => (m[n] && m[n].values[f] !== undefined ? m[n].values[f] : 0);
  const all = {
    rps: RPS,
    duration: DURATION,
    requests: g('http_reqs', 'count'),
    achieved_rps: g('http_reqs', 'rate'),
    p50: g('http_req_duration', 'med'),
    p95: g('http_req_duration', 'p(95)'),
    p99: g('http_req_duration', 'p(99)'),
    max: g('http_req_duration', 'max'),
    server_error_rate: g('server_errors', 'rate'),
    throttled: g('throttled', 'count'),
    network_failures: g('network_failures', 'count'),
    unexpected_status: g('unexpected_status', 'count'),
    dropped: g('dropped_iterations', 'count'),
    max_vus: g('vus_max', 'max'),
    rows,
  };
  const f = (x) => (x >= 100 ? x.toFixed(0) : x.toFixed(1)).padStart(7);
  let out = `\n  цель ${RPS} зап/с на ${DURATION}: получено ${all.achieved_rps.toFixed(0)} зап/с, всего ${all.requests}, потеряно (не успели отправить) ${all.dropped}\n`;
  out += `  все запросы: p50 ${f(all.p50)} мс, p95 ${f(all.p95)} мс, p99 ${f(all.p99)} мс, max ${f(all.max)} мс; 5xx ${(all.server_error_rate * 100).toFixed(3)}%, 429: ${all.throttled}, без ответа: ${all.network_failures}, неожиданный код: ${all.unexpected_status}\n\n`;
  out += '  ' + 'запрос'.padEnd(26) + 'кол-во'.padStart(8) + 'p50'.padStart(8) + 'p95'.padStart(8) + 'p99'.padStart(8) + 'max'.padStart(8) + 'бюджет'.padStart(8) + '\n';
  for (const r of rows) {
    out += '  ' + r.name.padEnd(26) + String(r.count).padStart(8) + f(r.p50) + f(r.p95) + f(r.p99) + f(r.max) + String(r.budget).padStart(8) + (r.ok ? '' : '  <-- выше бюджета') + '\n';
  }
  return { [SUMMARY_OUT]: JSON.stringify(all, null, 1), stdout: out };
}
