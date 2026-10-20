// Презентация ИОН для Бизнес-инкубатора ИТ-парка. Сборка: node build.js <out.pptx>
const fs = require('fs')
const path = require('path')
const pptxgen = require('pptxgenjs')
const React = require('react')
const { renderToStaticMarkup } = require('react-dom/server')
const sharp = require('sharp')
const lu = require('react-icons/lu')

const OUT = process.argv[2] || 'ion-pitch.pptx'
const A = (f) => path.join(__dirname, 'assets', f)

// Бренд (DESIGN.md)
const K = {
  navy: '1C2C66', navyD: '121E4D', wine: '722F37', green: '1E6B45',
  ink: '1F1D1A', body: '38352F', soft: '5D5A55', edge: '85807A', line: 'E4E1DC',
  wash: 'F6F4F1', wash2: 'ECE9E4', blueW: 'ECEEFA', wineW: 'F7EDEE', greenW: 'E6F3EC', white: 'FFFFFF',
  onInkSoft: 'C9CCE0',
}
const HEAD = 'Times New Roman'
const BODY = 'Calibri'
const THEME = {
  name: 'ИОН', headFontFace: HEAD, bodyFontFace: BODY,
  colors: { dk1: K.ink, lt1: K.white, dk2: K.navyD, lt2: K.wash, accent1: K.navy, accent2: K.wine, accent3: K.green, accent4: K.edge, accent5: '3B4E9E', accent6: 'B9848A', hlink: K.navy, folHlink: K.wine },
}
// Три сферы: И, О, Н
const SPH = {
  I: { letter: 'И', word: 'Инновации', color: K.green, wash: K.greenW },
  O: { letter: 'О', word: 'Образование', color: K.wine, wash: K.wineW },
  N: { letter: 'Н', word: 'Наука', color: K.navy, wash: K.blueW },
}

async function icon(name, color = 'FFFFFF', px = 256) {
  const Comp = lu[name]
  if (!Comp) throw new Error('нет иконки ' + name)
  const svg = renderToStaticMarkup(React.createElement(Comp, { size: px, color: '#' + color, strokeWidth: 1.75 }))
  const buf = await sharp(Buffer.from(svg)).png().toBuffer()
  return 'image/png;base64,' + buf.toString('base64')
}

function imgSize(file) {
  const b = fs.readFileSync(file)
  if (b[0] === 0x89) return { w: b.readUInt32BE(16), h: b.readUInt32BE(20) }
  let i = 2
  while (i < b.length) {
    const m = b[i + 1], len = b.readUInt16BE(i + 2)
    if (m >= 0xc0 && m <= 0xc3) return { h: b.readUInt16BE(i + 5), w: b.readUInt16BE(i + 7) }
    i += 2 + len
  }
  throw new Error('size ' + file)
}

async function main() {
  const pres = new pptxgen()
  pres.layout = 'LAYOUT_WIDE' // 13.333 × 7.5
  pres.title = 'ИОН — Инновации, образование, наука'
  pres.subject = 'Заявка в Бизнес-инкубатор ИТ-парка, осенний отбор 2026'
  pres.author = 'Команда ИОН: М. А. Мухамедьяров, А. И. Сибгатуллин'
  pres.company = 'ИОН'
  pres.theme = { headFontFace: HEAD, bodyFontFace: BODY }

  const W = 13.333, M = 0.6
  const logoSz = imgSize(A('logo.png'))
  const logoW = (h) => (h * logoSz.w) / logoSz.h

  pres.defineSlideMaster({
    title: 'CONTENT',
    background: { color: K.white },
    margin: [0.5, 0.6, 0.6, 0.6],
    objects: [
      { image: { path: A('logo.png'), x: W - M - logoW(0.36), y: 0.42, w: logoW(0.36), h: 0.36 } },
      { text: { text: 'ИОН · Инновации, образование, наука     Отбор в Бизнес-инкубатор ИТ-парка, осень 2026', options: { x: M, y: 7.02, w: 9, h: 0.28, fontFace: BODY, fontSize: 9, color: K.edge, margin: 0 } } },
      { placeholder: { options: { name: 'title', type: 'title', x: M, y: 0.86, w: 11.6, h: 0.82, fontFace: HEAD, fontSize: 26, bold: true, color: K.ink, valign: 'top', align: 'left', margin: 0 }, text: '' } },
    ],
    slideNumber: { x: W - M - 0.6, y: 7.02, w: 0.6, h: 0.28, fontFace: BODY, fontSize: 9, color: K.edge, align: 'right', margin: 0 },
  })
  pres.defineSlideMaster({ title: 'COVER', background: { color: K.white }, objects: [] })
  pres.defineSlideMaster({ title: 'DARK', background: { color: K.navyD }, objects: [] })

  // ---------- помощники ----------
  const T = (s, text, o) => s.addText(text, { isTextBox: true, fontFace: BODY, color: K.body, fontSize: 14, margin: 0, valign: 'top', ...o })
  const chip = (s, text, x = M, y = 0.46, fill = K.blueW, color = K.navy) => {
    const w = 0.08 * text.length + 0.42
    s.addShape(pres.shapes.ROUNDED_RECTANGLE, { x, y, w, h: 0.28, rectRadius: 0.14, fill: { color: fill }, line: { type: 'none' }, objectName: 'chip' })
    T(s, text.toUpperCase(), { x, y, w, h: 0.28, fontSize: 9.5, bold: true, color, align: 'center', valign: 'middle', charSpacing: 1.2 })
  }
  const title = (s, text) => s.addText(text, { placeholder: 'title' })
  const lead = (s, text, y = 1.66, w = 11.2) => T(s, text, { x: M, y, w, h: 0.5, fontSize: 15, color: K.soft })
  const source = (s, text, y = 6.62) => T(s, text, { x: M, y, w: W - 2 * M, h: 0.34, fontSize: 9, color: K.edge, valign: 'bottom' })
  const card = (s, x, y, w, h, fill = K.wash, line) => s.addShape(pres.shapes.ROUNDED_RECTANGLE, { x, y, w, h, rectRadius: 0.08, fill: { color: fill }, line: line ? { color: line, width: 0.75 } : { type: 'none' } })
  const circle = (s, x, y, d, fill, line) => s.addShape(pres.shapes.OVAL, { x, y, w: d, h: d, fill: { color: fill }, line: line ? { color: line, width: 1.5 } : { type: 'none' } })
  const iconCircle = async (s, name, x, y, d, fill, fg = 'FFFFFF') => {
    circle(s, x, y, d, fill)
    const p = d * 0.25
    s.addImage({ data: await icon(name, fg), x: x + p, y: y + p, w: d - 2 * p, h: d - 2 * p })
  }
  const badge = (s, key, x, y, d = 0.42, fs = 15) => {
    circle(s, x, y, d, SPH[key].color)
    T(s, SPH[key].letter, { x, y, w: d, h: d, fontFace: HEAD, fontSize: fs, bold: true, color: K.white, align: 'center', valign: 'middle' })
  }
  const shot = (s, file, x, y, w, maxH) => {
    const sz = imgSize(A(file))
    let h = (w * sz.h) / sz.w
    let ww = w
    if (maxH && h > maxH) { h = maxH; ww = (h * sz.w) / sz.h }
    s.addShape(pres.shapes.RECTANGLE, { x: x - 0.02, y: y - 0.02, w: ww + 0.04, h: h + 0.04, fill: { color: K.white }, line: { color: K.line, width: 1 }, shadow: { type: 'outer', color: '1F1D1A', opacity: 0.12, blur: 10, offset: 3, angle: 90 } })
    s.addImage({ path: A(file), x, y, w: ww, h })
    return { w: ww, h }
  }
  const bullets = (s, items, o) => s.addText(items.map((t, i) => ({ text: t, options: { bullet: { indent: 14 }, breakLine: i < items.length - 1, paraSpaceAfter: 5 } })), { isTextBox: true, fontFace: BODY, color: K.body, fontSize: 14, margin: 0, valign: 'top', ...o })

  const slide = (section, master = 'CONTENT') => pres.addSlide({ masterName: master, sectionTitle: section })

  // ================= 1. ТИТУЛ =================
  pres.addSection({ title: 'Проект' })
  {
    const s = slide('Проект', 'COVER')
    const lh = 1.05
    s.addImage({ path: A('logo.png'), x: M, y: 0.62, w: logoW(lh), h: lh, altText: 'Логотип ИОН' })
    chip(s, 'Заявка в Бизнес-инкубатор ИТ-парка · осень 2026', M, 2.08)
    T(s, 'Платформа для карьеры в науке, образовании и инновациях', { x: M, y: 2.52, w: 7.3, h: 1.45, fontFace: HEAD, fontSize: 34, bold: true, color: K.ink, valign: 'top' })
    T(s, 'Вакансии вузов, НИИ и R&D-компаний, научный профиль вместо резюме и переписка соискателя с работодателем в одном месте', { x: M, y: 4.02, w: 7.0, h: 0.8, fontSize: 16, color: K.soft })
    // команда
    const ty = 5.25
    const people = [
      { img: fs.existsSync(A('marat-sq.jpg')) ? 'marat-sq.jpg' : null, ini: 'ММ', name: 'Марат Мухамедьяров', role: 'д.м.н., профессор, Казанский ГМУ' },
      { img: 'azamat-sq.jpg', ini: 'АС', name: 'Азамат Сибгатуллин', role: 'разработчик платформы' },
    ]
    people.forEach((p, i) => {
      const x = M + i * 3.6
      if (p.img) { s.addImage({ path: A(p.img), x, y: ty, w: 0.78, h: 0.78, rounding: true, sizing: { type: 'cover', w: 0.78, h: 0.78 } }); s.addShape(pres.shapes.OVAL, { x, y: ty, w: 0.78, h: 0.78, fill: { type: 'none' }, line: { color: K.line, width: 1.25 } }) }
      else { circle(s, x, ty, 0.78, K.blueW); T(s, p.ini, { x, y: ty, w: 0.78, h: 0.78, fontFace: HEAD, fontSize: 18, bold: true, color: K.navy, align: 'center', valign: 'middle' }) }
      T(s, p.name, { x: x + 0.95, y: ty + 0.08, w: 2.55, h: 0.32, fontSize: 14, bold: true, color: K.ink })
      T(s, p.role, { x: x + 0.95, y: ty + 0.4, w: 2.55, h: 0.32, fontSize: 12, color: K.soft })
    })
    T(s, 'Казань · 2026', { x: M, y: 6.75, w: 3, h: 0.3, fontSize: 11, color: K.edge })
    // правая панель: треугольник знаний
    const px = 8.55
    s.addShape(pres.shapes.RECTANGLE, { x: px, y: 0, w: W - px, h: 7.5, fill: { color: K.navyD }, line: { type: 'none' } })
    T(s, 'ТРЕУГОЛЬНИК ЗНАНИЙ', { x: px + 0.6, y: 0.95, w: 3.8, h: 0.3, fontSize: 10, bold: true, color: K.onInkSoft, charSpacing: 2 })
    const rows = [
      ['I', 'R&D-компании, технопарки, проектная работа и трансфер технологий'],
      ['O', 'Преподаватели вузов, аспирантура, магистратура, стажировки'],
      ['N', 'Научные должности, конкурсы, гранты, публикации'],
    ]
    rows.forEach(([k, d], i) => {
      const y = 1.6 + i * 1.72
      circle(s, px + 0.6, y, 0.78, SPH[k].color, 'FFFFFF')
      T(s, SPH[k].letter, { x: px + 0.6, y, w: 0.78, h: 0.78, fontFace: HEAD, fontSize: 26, bold: true, color: K.white, align: 'center', valign: 'middle' })
      T(s, SPH[k].word, { x: px + 1.6, y: y - 0.02, w: 2.9, h: 0.5, fontFace: HEAD, fontSize: 24, bold: true, color: K.white })
      T(s, d, { x: px + 1.6, y: y + 0.5, w: 2.95, h: 0.8, fontSize: 13, color: K.onInkSoft })
    })
    s.addNotes('Добрый день. Мы команда ИОН: «Инновации, образование, наука». Это онлайн-платформа, где учёные, преподаватели и инноваторы ищут работу и места в аспирантуре, а вузы, НИИ и R&D-компании находят кандидатов. Первая версия уже разработана; в инкубаторе мы хотим превратить её в бизнес.')
  }

  // ================= 2. ПРОБЛЕМА =================
  {
    const s = slide('Проект')
    chip(s, 'Проблема')
    title(s, 'Сервисы поиска работы не говорят на языке науки')
    const cols = [
      { icon: 'LuGraduationCap', who: 'Учёный, преподаватель, аспирант', fill: K.blueW, c: K.navy, items: [
        'Обычное резюме не вмещает степень, звание, публикации, гранты и индексы цитирования',
        'Конкурсы и места в аспирантуре разбросаны по сайтам вузов и НИИ, сроки подачи легко пропустить',
        'Общие сайты не ищут по научной специальности и степени, поэтому учёные почти не считают их рабочим инструментом',
      ] },
      { icon: 'LuBuilding2', who: 'Вуз, НИИ, R&D-компания', fill: K.wineW, c: K.wine, items: [
        'Нельзя найти кандидата по специальности ВАК, степени, h-index и публикациям',
        'Отклики, документы и рекомендательные письма приходят по почте и теряются',
        'У кафедр и лабораторий нет общего инструмента найма: каждая ищет людей сама',
      ] },
    ]
    for (let i = 0; i < 2; i++) {
      const c = cols[i], x = M + i * 6.15, y = 1.95, w = 5.95
      card(s, x, y, w, 3.7, c.fill)
      await iconCircle(s, c.icon, x + 0.35, y + 0.32, 0.62, c.c)
      T(s, c.who, { x: x + 1.15, y: y + 0.4, w: w - 1.4, h: 0.45, fontFace: HEAD, fontSize: 20, bold: true, color: K.ink, valign: 'middle' })
      c.items.forEach((t, j) => {
        const yy = y + 1.2 + j * 0.8
        T(s, String(j + 1), { x: x + 0.42, y: yy, w: 0.45, h: 0.45, fontFace: HEAD, fontSize: 22, bold: true, color: c.c })
        T(s, t, { x: x + 1.15, y: yy + 0.02, w: w - 1.45, h: 0.72, fontSize: 14, color: K.body })
      })
    }
    card(s, M, 5.85, W - 2 * M, 0.72, K.navyD)
    T(s, [{ text: 'Итог: ', options: { bold: true, color: K.white } }, { text: 'учёные ищут работу через знакомых и сайты отдельных вузов, а организации теряют кандидатов, до которых не дотянулись.', options: { color: K.white } }], { x: M + 0.35, y: 5.85, w: W - 2 * M - 0.7, h: 0.72, fontSize: 15, valign: 'middle' })
    source(s, 'Опыт команды в Казанском ГМУ; масштаб проблемы проверим 20 интервью в инкубаторе', 6.62)
    s.addNotes('Проблема двусторонняя. Учёному негде показать свой научный багаж: степень, публикации, гранты не помещаются в обычное резюме, а конкурсы разбросаны по сотням сайтов. Организации, наоборот, не могут найти человека по научной специальности и публикациям, а отклики и рекомендации ходят по почте.')
  }

  // ================= 3. АКТУАЛЬНОСТЬ =================
  {
    const s = slide('Проект')
    chip(s, 'Актуальность')
    title(s, 'Рынок большой, и он живёт конкурсами')
    const stats = [
      ['339,1', 'тыс.', 'исследователей в организациях науки (2024)', K.navy],
      ['217,0', 'тыс.', 'преподавателей вузов, ППС (2024/25)', K.wine],
      ['125,9', 'тыс.', 'аспирантов (2024)', K.navy],
      ['4,1', 'тыс.+', 'организаций ведут исследования и разработки, плюс 724 вуза (2023–2024)', K.green],
    ]
    const sw = (W - 2 * M - 3 * 0.3) / 4
    stats.forEach(([n, u, d, c], i) => {
      const x = M + i * (sw + 0.3), y = 1.95
      card(s, x, y, sw, 2.0, K.wash)
      T(s, [{ text: n, options: { fontSize: 40, bold: true, color: c, fontFace: HEAD } }, { text: ' ' + u, options: { fontSize: 18, bold: true, color: c, fontFace: HEAD } }], { x: x + 0.3, y: y + 0.25, w: sw - 0.5, h: 0.75, valign: 'middle' })
      T(s, d, { x: x + 0.3, y: y + 1.05, w: sw - 0.5, h: 0.8, fontSize: 13, color: K.body })
    })
    const facts = [
      ['LuFileSearch', '≈ 15 тыс. конкурсов в год', 'только на научные должности: организации обязаны публиковать их на портале «ученые-исследователи.рф» (приказ Минобрнауки № 715). Оценка по номерам вакансий: № 133 014 в ноябре 2024 → № 148 045 в ноябре 2025'],
      ['LuUsers', '43 % исследователей моложе 40 лет', 'поколение, которое привыкло искать работу и общаться с работодателем онлайн'],
      ['LuCalendarRange', '2022–2031: Десятилетие науки и технологий', 'государство ставит задачу привлечь молодёжь в науку и закрыть кадровый дефицит'],
    ]
    const fw = (W - 2 * M - 2 * 0.3) / 3
    for (let i = 0; i < 3; i++) {
      const [ic, h, d] = facts[i], x = M + i * (fw + 0.3), y = 4.2
      card(s, x, y, fw, 2.25, K.white, K.line)
      await iconCircle(s, ic, x + 0.28, y + 0.28, 0.5, K.navy)
      T(s, h, { x: x + 0.92, y: y + 0.26, w: fw - 1.15, h: 0.55, fontSize: 15, bold: true, color: K.ink, valign: 'middle' })
      T(s, d, { x: x + 0.28, y: y + 0.92, w: fw - 0.5, h: 1.25, fontSize: 12, color: K.soft })
    }
    source(s, 'Источники: ИСИЭЗ НИУ ВШЭ, «Кадры крупных и средних организаций науки: итоги 2024 года» и «Позитивные тенденции в аспирантуре закрепляются» (2025); Росстат, Российский статистический ежегодник 2025; доклад Минобрнауки о высшем образовании (2023); Указ Президента РФ № 231 от 25.04.2022')
    s.addNotes('Масштаб: 339 тысяч исследователей, 217 тысяч преподавателей вузов, 126 тысяч аспирантов, больше 4 тысяч организаций, которые ведут исследования. Замещение научных должностей идёт через конкурсы: по нашей оценке, только на государственный портал выкладывается около 15 тысяч конкурсов в год. Почти половина исследователей моложе 40 лет: это аудитория, которая ищет работу онлайн.')
  }

  // ================= 4. РЕШЕНИЕ =================
  {
    const s = slide('Проект')
    chip(s, 'Решение')
    title(s, 'ИОН соединяет науку, образование и инновации')
    // треугольник
    const cx = 3.45, top = 2.0, d = 1.25
    const P = { N: [cx - d / 2, top], O: [M + 0.35, 4.75], I: [cx * 2 - M - 0.35 - d, 4.75] }
    const ctr = (k) => [P[k][0] + d / 2, P[k][1] + d / 2]
    const ln = (a, b) => { const [x1, y1] = ctr(a), [x2, y2] = ctr(b); s.addShape(pres.shapes.LINE, { x: Math.min(x1, x2), y: Math.min(y1, y2), w: Math.abs(x2 - x1) || 0.01, h: Math.abs(y2 - y1) || 0.01, flipH: (x2 < x1) !== (y2 < y1), line: { color: K.edge, width: 1.25, dashType: 'dash' } }) }
    ln('N', 'O'); ln('N', 'I'); ln('O', 'I')
    for (const k of ['N', 'O', 'I']) {
      const [x, y] = P[k]
      circle(s, x, y, d, SPH[k].color, 'FFFFFF')
      T(s, SPH[k].letter, { x, y, w: d, h: d, fontFace: HEAD, fontSize: 40, bold: true, color: K.white, align: 'center', valign: 'middle' })
    }
    T(s, 'Наука', { x: P.N[0] + d + 0.15, y: P.N[1] + 0.35, w: 1.8, h: 0.5, fontFace: HEAD, fontSize: 18, bold: true, color: K.navy })
    T(s, 'Образование', { x: P.O[0] - 0.2, y: P.O[1] + d + 0.08, w: 1.9, h: 0.4, fontFace: HEAD, fontSize: 17, bold: true, color: K.wine, align: 'center' })
    T(s, 'Инновации', { x: P.I[0] - 0.3, y: P.I[1] + d + 0.08, w: 1.9, h: 0.4, fontFace: HEAD, fontSize: 17, bold: true, color: K.green, align: 'center' })
    T(s, 'один профиль\nвсе виды позиций\nобщая переписка', { x: cx - 1.0, y: 3.85, w: 2.0, h: 0.95, fontSize: 12, color: K.soft, align: 'center', valign: 'middle', italic: true })
    // правая часть
    const rx = 6.85, rw = W - M - rx
    const blocks = [
      ['N', 'Наука', 'Научные должности от лаборанта-исследователя до заведующего лабораторией, постдоки, конкурсы со сроками, гранты'],
      ['O', 'Образование', 'Преподаватели и заведующие кафедрами, места в аспирантуре под тему и руководителя, магистратура, стажировки'],
      ['I', 'Инновации', 'R&D-компании и технопарки: проектная работа по грантам и НИОКР, трансфер технологий, грант-менеджмент'],
    ]
    blocks.forEach(([k, h, t], i) => {
      const y = 1.85 + i * 1.47
      card(s, rx, y, rw, 1.35, SPH[k].wash)
      badge(s, k, rx + 0.25, y + 0.25, 0.5, 18)
      T(s, h, { x: rx + 0.95, y: y + 0.22, w: rw - 1.2, h: 0.4, fontFace: HEAD, fontSize: 17, bold: true, color: K.ink })
      T(s, t, { x: rx + 0.95, y: y + 0.62, w: rw - 1.2, h: 0.65, fontSize: 13, color: K.body })
    })
    T(s, 'Сервис ищет вакансии и соискателей с учётом специфики научно-образовательной сферы: специальности ВАК, степени и звания, публикации, сроки конкурсов.', { x: rx, y: 6.3, w: rw, h: 0.5, fontSize: 12, color: K.soft, italic: true })
    s.addNotes('Наше решение — платформа для всего «треугольника знаний». Наука: научные должности и конкурсы. Образование: преподаватели, аспирантура, магистратура, стажировки. Инновации: R&D-компании и технопарки, проектная работа и трансфер технологий. В центре — профиль учёного, вакансии всех видов и переписка.')
  }

  // ================= 5. КАК РАБОТАЕТ =================
  pres.addSection({ title: 'Продукт' })
  {
    const s = slide('Продукт')
    chip(s, 'Как это работает')
    title(s, 'Один профиль вместо резюме, один кабинет на всю организацию')
    const lanes = [
      { who: 'Соискатель', ic: 'LuUserRound', c: K.navy, wash: K.blueW, steps: [
        ['Профиль', 'Образование, степень, публикации по DOI с квартилем журнала, гранты, ORCID, SPIN, Scopus'],
        ['Поиск и подбор', 'Фильтры по специальности ВАК, степени, региону, условиям; подборка с объяснением'],
        ['Отклик', 'Письмо, файлы, рекомендации по одноразовой ссылке; резюме PDF собирается само'],
        ['Решение', 'Переписка с организацией, статусы отклика, напоминания о сроках подачи'],
      ] },
      { who: 'Организация', ic: 'LuLandmark', c: K.wine, wash: K.wineW, steps: [
        ['Кабинет', 'Подразделения и роли: владелец, кадровик, руководитель лаборатории или кафедры'],
        ['Вакансия', 'Научные условия: оборудование и ЦКП, оплата публикаций, конкурс и срок'],
        ['Отклики', 'Снимок профиля, рекомендательные письма, выгрузка в таблицу, журнал действий'],
        ['Поиск людей', 'Каталог учёных по специальности, степени, h-index; приглашение на вакансию'],
      ] },
    ]
    const lx = M + 1.75, gw = (W - M - lx - 3 * 0.22) / 4
    for (let li = 0; li < 2; li++) {
      const L = lanes[li], y = 1.95 + li * 2.2
      await iconCircle(s, L.ic, M + 0.35, y + 0.35, 0.7, L.c)
      T(s, L.who, { x: M, y: y + 1.12, w: 1.45, h: 0.4, fontSize: 14, bold: true, color: L.c, align: 'center' })
      L.steps.forEach(([h, t], j) => {
        const x = lx + j * (gw + 0.22)
        card(s, x, y, gw, 1.95, L.wash)
        circle(s, x + 0.22, y + 0.22, 0.42, L.c)
        T(s, String(j + 1), { x: x + 0.22, y: y + 0.22, w: 0.42, h: 0.42, fontSize: 14, bold: true, color: K.white, align: 'center', valign: 'middle' })
        T(s, h, { x: x + 0.78, y: y + 0.22, w: gw - 0.95, h: 0.42, fontSize: 15, bold: true, color: K.ink, valign: 'middle' })
        T(s, t, { x: x + 0.22, y: y + 0.78, w: gw - 0.42, h: 1.1, fontSize: 12.5, color: K.body })
      })
    }
    card(s, M, 6.38, W - 2 * M, 0.52, K.wash)
    await (async () => { s.addImage({ data: await icon('LuShieldCheck', K.green), x: M + 0.22, y: 6.47, w: 0.34, h: 0.34 }) })()
    T(s, [{ text: 'Приватность решает соискатель: ', options: { bold: true, color: K.ink } }, { text: 'профиль скрыт, анонимен, виден организациям или всем; можно скрыть его от своей организации.', options: {} }], { x: M + 0.7, y: 6.38, w: W - 2 * M - 0.9, h: 0.52, fontSize: 13, valign: 'middle' })
    s.addNotes('Как это работает. Соискатель один раз заполняет научный профиль: публикации подтягиваются по DOI вместе с квартилем журнала. Дальше поиск, подборка с объяснением, отклик с рекомендательными письмами и переписка. Организация заводит подразделения и роли, публикует вакансии, разбирает отклики и сама ищет учёных в каталоге. Видимость профиля всегда выбирает человек.')
  }

  // ================= 6. ПРОДУКТ: ЭКРАНЫ =================
  {
    const s = slide('Продукт')
    chip(s, 'Продукт')
    title(s, 'Первая версия разработана и работает')
    const items0 = null
    const items = [
      ['s-search.jpg', 'Поиск вакансий', 'русская морфология, фильтры по области науки, виду позиции, уровню, формату работы'],
      ['s-scientists.jpg', 'Каталог учёных', 'организация ищет по степени, званию, h-index, статьям в Q1–Q2 и приглашает на вакансию'],
      ['s-candidate.jpg', 'Разбор отклика', 'письмо, файлы, рекомендации, снимок профиля; принять, отказать или написать'],
    ]
    const iw = (W - 2 * M - 2 * 0.35) / 3
    items.forEach(([f, h, d], i) => {
      const x = M + i * (iw + 0.35), y = 1.9
      const r = shot(s, f, x, y, iw, 3.5)
      T(s, h, { x, y: y + r.h + 0.2, w: iw, h: 0.38, fontSize: 16, bold: true, color: K.ink })
      T(s, d, { x, y: y + r.h + 0.58, w: iw, h: 0.75, fontSize: 13, color: K.soft })
    })
    source(s, 'Скриншоты работающего сайта. Организации и люди в демонстрационных данных вымышленные, города настоящие')
    s.addNotes('Это не макеты, а работающий сайт. Слева поиск вакансий с фильтрами, в центре каталог учёных для организаций: поиск по степени, h-index и статьям в первом-втором квартиле. Справа разбор отклика: письмо, файлы, рекомендации и решение в одно нажатие.')
  }

  // ================= 7. ПРОДУКТ: ЯЗЫК НАУКИ =================
  {
    const s = slide('Продукт')
    chip(s, 'Продукт')
    title(s, 'Сайт говорит на языке науки: квартили, специальности, сроки')
    const r = shot(s, 's-pubs.jpg', M, 1.9, 6.55)
    T(s, 'Публикации по DOI: журнал и квартиль подставляются сами по справочнику SCImago (32 тыс. журналов)', { x: M, y: 1.9 + r.h + 0.18, w: 6.55, h: 0.6, fontSize: 13, color: K.soft })
    const pw = 1.62, gap = 0.22, px0 = W - M - (3 * pw + 2 * gap)
    const phones = [['phone-vacancies.jpg', 'Поиск'], ['phone-matches.jpg', 'Подходящие вам'], ['phone-profile.jpg', 'Профиль']]
    phones.forEach(([f, cap], i) => {
      const x = px0 + i * (pw + gap)
      const ph = shot(s, f, x, 1.9, pw)
      T(s, cap, { x, y: 1.9 + ph.h + 0.12, w: pw, h: 0.3, fontSize: 12, color: K.soft, align: 'center' })
    })
    const feats = [['LuBellRing', 'Напоминания за 7 дней и за 1 день до срока подачи'], ['LuFileText', 'Резюме PDF из профиля одним нажатием'], ['LuSmartphone', 'На телефоне так же удобно, как за компьютером']]
    for (let i = 0; i < 3; i++) {
      const x = px0 + i * (pw + gap), y = 5.85
      s.addImage({ data: await icon(feats[i][0], K.navy), x, y, w: 0.3, h: 0.3 })
      T(s, feats[i][1], { x, y: y + 0.36, w: pw, h: 0.7, fontSize: 11.5, color: K.body })
    }
    s.addNotes('Главное отличие — сайт говорит на языке науки. Публикацию достаточно указать по DOI: название, авторы, журнал и квартиль подставятся сами. Подборка «Подходящие вам» объясняет, почему вакансия в списке. Есть напоминания о сроках конкурсов, резюме в PDF и полноценная работа с телефона.')
  }

  // ================= 8. КОНКУРЕНТЫ =================
  pres.addSection({ title: 'Рынок' })
  {
    const s = slide('Рынок')
    chip(s, 'Конкуренты')
    title(s, 'Общие сайты не знают науку, госпортал знает только конкурсы')
    const Y = '●', P = '◐', N = '—'
    const head = ['Возможность', 'ИОН', 'hh.ru, SuperJob, «Работа России»', 'Портал «ученые-исследователи.рф»', 'Сайты вузов и каналы в мессенджерах']
    const rows = [
      ['Научные должности, ППС, аспирантура, магистратура, стажировки, проекты', Y, P, P, P],
      ['Поиск по специальности ВАК, степени и званию', Y, N, P, N],
      ['Научный профиль: публикации с квартилями, h-index, ORCID и SPIN, гранты', Y, N, P, N],
      ['Рекомендательные письма через платформу', Y, N, N, N],
      ['Сроки подачи и напоминания о них', Y, N, P, N],
      ['Каталог учёных для работодателя и приглашение на вакансию', Y, P, N, N],
      ['Переписка соискателя и работодателя', Y, Y, N, N],
    ]
    const hdr = head.map((h, i) => ({ text: h, options: { bold: true, color: i === 1 ? K.white : K.ink, fill: { color: i === 1 ? K.navy : K.wash2 }, align: i === 0 ? 'left' : 'center', valign: 'middle', fontSize: 12 } }))
    const body = rows.map((r) => r.map((c, i) => ({ text: c, options: { align: i === 0 ? 'left' : 'center', valign: 'middle', fontSize: i === 0 ? 12.5 : 16, bold: i === 1, color: i === 0 ? K.body : (c === Y ? (i === 1 ? K.navy : K.body) : c === P ? K.soft : K.edge), fill: { color: i === 1 ? K.blueW : K.white } } })))
    s.addTable([hdr, ...body], { x: M, y: 1.85, w: W - 2 * M, colW: [4.73, 1.35, 2.0, 2.0, 2.05], rowH: 0.46, fontFace: BODY, border: { type: 'solid', color: K.line, pt: 0.75 }, margin: [0.04, 0.12, 0.04, 0.12] })
    T(s, '● есть   ◐ частично   — нет', { x: M, y: 6.0, w: 4, h: 0.3, fontSize: 11, color: K.soft })
    card(s, 4.4, 5.82, W - M - 4.4, 0.8, K.blueW)
    T(s, [{ text: 'Госпортал обязателен для научных конкурсов (приказ № 715), но в нём нет ППС, аспирантуры и R&D-компаний. ' }, { text: 'Наша защита — сетевой эффект: ', options: { bold: true } }, { text: 'каждый профиль учёного делает каталог ценнее для организаций, и наоборот.' }], { x: 4.62, y: 5.82, w: W - M - 4.84, h: 0.8, fontSize: 12, color: K.navyD, valign: 'middle' })
    source(s, 'Сравнение по открытым данным на октябрь 2026 года; уточним на интервью с кадровыми службами')
    s.addNotes('Конкуренты. Общие сайты, hh.ru и другие, умеют переписку и приватность, но не знают степеней, специальностей ВАК и публикаций. Государственный портал обязателен для научных конкурсов, но это доска объявлений о конкурсах без преподавателей, аспирантуры и частных компаний. Сайты вузов и телеграм-каналы — каждый сам по себе. ИОН соединяет это в одном месте и не спорит с порталом, а дополняет его.')
  }

  // ================= 9. РЫНОК =================
  {
    const s = slide('Рынок')
    chip(s, 'Рынок')
    title(s, 'Начинаем с вузов: рынок организаций около 0,6 млрд ₽ в год')
    // круги
    const bx = M + 0.1, by = 1.95, D = 4.4
    circle(s, bx, by, D, K.blueW)
    circle(s, bx + D * 0.2, by + D * 0.4, D * 0.6, 'C9CFEA')
    circle(s, bx + D * 0.36, by + D * 0.72, D * 0.28, K.navy)
    T(s, 'TAM', { x: bx, y: by + 0.3, w: D, h: 0.35, fontSize: 14, bold: true, color: K.navy, align: 'center' })
    T(s, 'SAM', { x: bx, y: by + D * 0.4 + 0.25, w: D, h: 0.35, fontSize: 14, bold: true, color: K.navyD, align: 'center' })
    T(s, 'SOM', { x: bx + D * 0.36, y: by + D * 0.72, w: D * 0.28, h: D * 0.28, fontSize: 13, bold: true, color: K.white, align: 'center', valign: 'middle' })
    const rx = 5.55, rw = W - M - rx
    const rows = [
      ['TAM', '≈ 0,6 млрд ₽ в год', '≈ 5 тыс. организаций: 724 вуза, 524 филиала, 4,1 тыс. организаций, ведущих исследования (с учётом пересечений) × 120 тыс. ₽ в год', K.navy],
      ['SAM', '≈ 150 млн ₽ в год', 'вузы и филиалы, ≈ 1,25 тыс.: каждый год нанимают преподавателей и набирают в аспирантуру и магистратуру × 120 тыс. ₽', K.navyD],
      ['SOM', '≈ 5 млн ₽ в год', 'к концу третьего года: 40 организаций (≈ 3 % SAM), начиная с Татарстана и Поволжья', K.wine],
    ]
    rows.forEach(([k, v, d, c], i) => {
      const y = 1.95 + i * 1.28
      T(s, k, { x: rx, y, w: 0.85, h: 0.45, fontSize: 15, bold: true, color: c })
      T(s, v, { x: rx + 0.85, y, w: rw - 0.85, h: 0.45, fontFace: HEAD, fontSize: 22, bold: true, color: K.ink })
      T(s, d, { x: rx + 0.85, y: y + 0.47, w: rw - 0.85, h: 0.7, fontSize: 12.5, color: K.body })
    })
    card(s, rx, 5.82, rw, 0.78, K.wash)
    T(s, [{ text: 'Цена 120 тыс. ₽ в год на организацию — гипотеза. ', options: { bold: true, color: K.ink } }, { text: 'Ориентир: средний чек hh.ru у малого и среднего бизнеса ≈ 36 тыс. ₽, у крупных клиентов ≈ 907 тыс. ₽ в год. Аудитория соискателей ≈ 680 тыс. человек.', options: {} }], { x: rx + 0.25, y: 5.82, w: rw - 0.5, h: 0.78, fontSize: 12, color: K.body, valign: 'middle' })
    source(s, 'Источники: ИСИЭЗ НИУ ВШЭ (2025), Минобрнауки (2023), МКПАО «Хэдхантер», отчётность за 2024 и 2025 годы (расчёт среднего чека МСБ: 20 млрд ₽ / 549 тыс. клиентов). Расчёт рынка — оценка команды')
    s.addNotes('Рынок считаем снизу вверх и честно помечаем гипотезы. Платят организации. Их около пяти тысяч; при цене 120 тысяч рублей в год это около 0,6 миллиарда рублей. Начинаем с вузов — это около 150 миллионов. Реалистичная цель на три года — 40 организаций, около 5 миллионов рублей в год. Цену проверим в инкубаторе.')
  }

  // ================= 10. БИЗНЕС-МОДЕЛЬ =================
  {
    const s = slide('Рынок')
    chip(s, 'Бизнес-модель')
    title(s, 'Платят организации, соискатели пользуются без оплаты')
    lead(s, 'Четыре гипотезы заработка; в инкубаторе проверяем, какая из них работает первой')
    const hyp = [
      [['O', 'N'], 'LuBriefcase', 'Кабинет найма для вуза и НИИ', 'Подписка на организацию: вакансии всех кафедр и лабораторий, роли, отклики, выгрузки, переписка', 'гипотеза: от 10 тыс. ₽ в месяц'],
      [['N'], 'LuSearch', 'Доступ к каталогу учёных', 'Поиск по специальности, степени, h-index и публикациям, приглашения на вакансию пакетами', 'как «доступ к базе» у hh.ru'],
      [['O'], 'LuMegaphone', 'Продвижение программ', 'Аспирантура, магистратура и стажировки в приёмную кампанию: показ подходящим соискателям', 'оплата за кампанию'],
      [['I'], 'LuFlaskConical', 'Подбор под проект', 'R&D-компании и технопарки ищут исполнителей под грант или НИОКР', 'разовая оплата за проект'],
    ]
    const cw = (W - 2 * M - 0.3) / 2, ch = 1.68
    for (let i = 0; i < 4; i++) {
      const [ks, ic, h, d, price] = hyp[i]
      const x = M + (i % 2) * (cw + 0.3), y = 2.2 + Math.floor(i / 2) * (ch + 0.18)
      card(s, x, y, cw, ch, K.wash)
      await iconCircle(s, ic, x + 0.28, y + 0.28, 0.58, K.navy)
      T(s, h, { x: x + 1.05, y: y + 0.26, w: cw - 2.2, h: 0.4, fontSize: 16, bold: true, color: K.ink })
      ks.forEach((k, j) => badge(s, k, x + cw - 0.4 - j * 0.42, y + 0.25, 0.34, 12))
      T(s, d, { x: x + 1.05, y: y + 0.68, w: cw - 1.3, h: 0.62, fontSize: 12.5, color: K.body })
      T(s, price, { x: x + 1.05, y: y + 1.28, w: cw - 1.3, h: 0.3, fontSize: 12, bold: true, color: K.wine })
    }
    card(s, M, 5.98, W - 2 * M, 0.66, K.navyD)
    T(s, [{ text: 'Что проверим: ', options: { bold: true } }, { text: 'кто в вузе решает и платит (кадровая служба, проректор по науке, заведующий лабораторией) и сколько. Цель юнит-экономики: привлечь организацию дешевле 40 тыс. ₽ при выручке от неё от 120 тыс. ₽ в год (LTV:CAC ≥ 3).' }], { x: M + 0.3, y: 5.98, w: W - 2 * M - 0.6, h: 0.66, fontSize: 12.5, color: K.white, valign: 'middle' })
    s.addNotes('Модель: платят организации, для соискателей сервис без оплаты — так мы быстрее наберём аудиторию. Четыре гипотезы: подписка на кабинет найма, доступ к каталогу учёных, продвижение программ аспирантуры и магистратуры и подбор исполнителей под проекты R&D-компаний. Какая сработает первой, хотим проверить с трекером инкубатора.')
  }

  // ================= 11. ГОТОВНОСТЬ =================
  pres.addSection({ title: 'План' })
  {
    const s = slide('План')
    chip(s, 'Готовность')
    title(s, 'Технически продукт готов к первым пользователям')
    const st = [['40', 'завершённых этапов разработки'], ['98,4 %', 'кода сервера покрыто автотестами'], ['1 296', 'автотестов интерфейса сайта'], ['120', 'угроз разобрано в модели безопасности, открытых нет']]
    const sw = (W - 2 * M - 3 * 0.3) / 4
    st.forEach(([n, d], i) => {
      const x = M + i * (sw + 0.3)
      T(s, n, { x, y: 1.9, w: sw, h: 0.8, fontFace: HEAD, fontSize: 40, bold: true, color: K.navy })
      T(s, d, { x, y: 2.7, w: sw - 0.2, h: 0.6, fontSize: 13, color: K.soft })
    })
    const cols = [
      ['LuGauge', 'Масштаб', ['Рассчитан на всю науку России: 350 тыс. человек, 300 тыс. вакансий, пик 500 запросов в секунду', 'Несколько копий сервера, фоновые задачи отдельно']],
      ['LuShieldCheck', 'Безопасность и закон', ['152-ФЗ: согласие на обработку данных, хостинг в России', 'Проверка файлов антивирусом, журнал действий, права проверены для каждой роли']],
      ['LuDatabase', 'Справочники науки', ['Специальности ВАК, 32 тыс. журналов SCImago с квартилями, 919 навыков', 'Поиск с русской морфологией и опечатками']],
    ]
    const cw = (W - 2 * M - 2 * 0.3) / 3
    for (let i = 0; i < 3; i++) {
      const [ic, h, items] = cols[i], x = M + i * (cw + 0.3), y = 3.55
      card(s, x, y, cw, 2.55, K.wash)
      await iconCircle(s, ic, x + 0.28, y + 0.28, 0.52, K.navy)
      T(s, h, { x: x + 0.95, y: y + 0.3, w: cw - 1.1, h: 0.48, fontSize: 16, bold: true, color: K.ink, valign: 'middle' })
      bullets(s, items, { x: x + 0.3, y: y + 0.98, w: cw - 0.55, h: 1.5, fontSize: 12.5 })
    }
    T(s, [{ text: 'Осталось до запуска: ', options: { bold: true, color: K.wine } }, { text: 'боевой хостинг в России и почтовый сервис, ручная модерация организаций (спроектирована, ждёт решений по процессу).' }], { x: M, y: 6.25, w: W - 2 * M, h: 0.4, fontSize: 13, color: K.body })
    source(s, 'Технологии: сервер Go, база PostgreSQL, сайт React; данные проекта на 20 октября 2026', 6.62)
    s.addNotes('Техническая реализуемость — наша сильная сторона. За 40 этапов сделана вся основная функциональность, сервер покрыт тестами на 98 процентов, проведён аудит безопасности. Архитектура рассчитана на всю науку России. До запуска осталось выбрать хостинг в России и включить модерацию организаций.')
  }

  // ================= 12. ВЫХОД НА РЫНОК =================
  {
    const s = slide('План')
    chip(s, 'Выход на рынок')
    title(s, 'Старт в Казани: через вузы и научные сообщества команды')
    const steps = [
      ['LuMessagesSquare', 'Интервью', '20 разговоров с кадровиками вузов, заведующими кафедрами и лабораториями, аспирантами', 'ноябрь–декабрь 2026'],
      ['LuHandshake', 'Пилот', '3–5 организаций Казани публикуют вакансии и места в аспирантуре', 'I квартал 2027'],
      ['LuUsersRound', 'Наполнение', '300 профилей учёных через советы молодых учёных, научные общества, конференции', 'I–II квартал 2027'],
      ['LuWallet', 'Первые оплаты', '5–10 организаций на подписке, подтверждённая цена', 'III–IV квартал 2027'],
    ]
    const gw = (W - 2 * M - 3 * 0.35) / 4
    for (let i = 0; i < 4; i++) {
      const [ic, h, d, when] = steps[i], x = M + i * (gw + 0.35), y = 1.95
      card(s, x, y, gw, 2.75, i === 3 ? K.navyD : K.wash)
      await iconCircle(s, ic, x + 0.28, y + 0.28, 0.58, i === 3 ? K.wine : K.navy)
      T(s, when, { x: x + 0.28, y: y + 1.0, w: gw - 0.5, h: 0.3, fontSize: 11, bold: true, color: i === 3 ? K.onInkSoft : K.wine, charSpacing: 0.5 })
      T(s, h, { x: x + 0.28, y: y + 1.3, w: gw - 0.5, h: 0.42, fontFace: HEAD, fontSize: 19, bold: true, color: i === 3 ? K.white : K.ink })
      T(s, d, { x: x + 0.28, y: y + 1.75, w: gw - 0.5, h: 0.95, fontSize: 12.5, color: i === 3 ? K.onInkSoft : K.body })
      if (i < 3) s.addImage({ data: await icon('LuChevronRight', K.edge), x: x + gw + 0.05, y: y + 1.2, w: 0.25, h: 0.25 })
    }
    T(s, 'Каналы', { x: M, y: 4.98, w: 3, h: 0.35, fontSize: 15, bold: true, color: K.ink })
    const ch = [
      ['LuNetwork', 'Сеть команды в КГМУ, Движении молодых учёных РТ и Физиологическом обществе им. И. П. Павлова'],
      ['LuPresentation', 'Конференции и школы молодых учёных, дни карьеры в вузах'],
      ['LuSend', 'Тематические каналы о науке, грантах и аспирантуре'],
      ['LuBuilding', 'Партнёры и мероприятия ИТ-парка для R&D-компаний'],
    ]
    const cw = (W - 2 * M - 3 * 0.3) / 4
    for (let i = 0; i < 4; i++) {
      const x = M + i * (cw + 0.3), y = 5.42
      s.addImage({ data: await icon(ch[i][0], K.navy), x, y, w: 0.32, h: 0.32 })
      T(s, ch[i][1], { x: x + 0.45, y: y - 0.02, w: cw - 0.5, h: 1.0, fontSize: 12, color: K.body })
    }
    s.addNotes('Выход на рынок начинаем с Казани, где у команды есть связи: КГМУ, движение молодых учёных, научные общества. Сначала 20 интервью, затем пилот с 3–5 организациями, параллельно набираем профили учёных, и к концу 2027 года — первые платящие организации.')
  }

  // ================= 13. ДОРОЖНАЯ КАРТА =================
  {
    const s = slide('План')
    chip(s, 'Дорожная карта')
    title(s, 'Двенадцать месяцев: от пилота к первым платежам')
    const ms = [
      ['Ноябрь 2026', 'Инкубатор', ['Отбор и трекер', 'Выбор формы юр. лица с учётом условий грантов']],
      ['Декабрь 2026', 'Запуск', ['Хостинг в России и почта', 'Модерация организаций', 'Интервью']],
      ['I кв. 2027', 'Пилот', ['3–5 организаций', 'Заявка «Студенческий стартап» ФСИ']],
      ['II кв. 2027', 'Рост', ['300 профилей учёных', 'Проверка цены и кто платит']],
      ['III кв. 2027', 'Монетизация', ['Первые платные подписки', 'Продвижение аспирантуры']],
      ['IV кв. 2027', 'Масштаб', ['10 платящих организаций', 'Заявка «Старт-1» ФСИ']],
    ]
    const n = ms.length, bw = (W - 2 * M - 5 * 0.2) / 6, x0 = M + bw / 2, x1 = W - M - bw / 2, ly = 2.55
    s.addShape(pres.shapes.LINE, { x: x0, y: ly, w: x1 - x0, h: 0, line: { color: K.line, width: 2.5 } })
    const step = (x1 - x0) / (n - 1)
    ms.forEach(([when, h, items], i) => {
      const cx = x0 + i * step
      const c = i === 0 ? K.wine : K.navy
      circle(s, cx - 0.16, ly - 0.16, 0.32, c, 'FFFFFF')
      T(s, when, { x: cx - bw / 2, y: 1.92, w: bw, h: 0.3, fontSize: 12, bold: true, color: c, align: 'center' })
      card(s, cx - bw / 2, 2.95, bw, 2.6, i === 0 ? K.wineW : K.wash)
      T(s, h, { x: cx - bw / 2 + 0.18, y: 3.12, w: bw - 0.36, h: 0.4, fontFace: HEAD, fontSize: 17, bold: true, color: K.ink })
      bullets(s, items, { x: cx - bw / 2 + 0.18, y: 3.62, w: bw - 0.32, h: 2.2, fontSize: 12.5 })
    })
    source(s, '«Студенческий стартап»: в 2026 году приём шёл с 9 февраля по 20 марта; условия очередей ФСИ сверяем по положению конкретного конкурса')
    s.addNotes('Дорожная карта на год. Ноябрь — инкубатор. Декабрь — боевой запуск и модерация организаций. Первый квартал — пилот и заявка на «Студенческий стартап». Дальше — профили учёных, проверка цены, первые подписки и к концу 2027 года десять платящих организаций и заявка на «Старт-1».')
  }

  // ================= 14. ФИНАНСЫ =================
  {
    const s = slide('План')
    chip(s, 'Финансы')
    title(s, 'Первый год укладывается в 1 млн ₽')
    lead(s, 'План бюджета для обсуждения с трекером; основная работа уже сделана силами команды')
    const rows = [
      ['Хостинг в России, почта, хранилище файлов, антивирус', '0,15'],
      ['Разработка и поддержка: выплаты команде', '0,55'],
      ['Интервью и пилоты: поездки, конференции, материалы', '0,10'],
      ['Маркетинг: мероприятия, публикации, продвижение', '0,12'],
      ['Юридическое: оферта, политика данных, регистрация', '0,08'],
    ]
    const tbl = [[{ text: 'Статья', options: { bold: true, fill: { color: K.wash2 } } }, { text: 'млн ₽', options: { bold: true, align: 'right', fill: { color: K.wash2 } } }],
      ...rows.map(([a, b]) => [{ text: a }, { text: b, options: { align: 'right' } }]),
      [{ text: 'Итого за 12 месяцев', options: { bold: true, color: K.white, fill: { color: K.navy } } }, { text: '1,00', options: { bold: true, align: 'right', color: K.white, fill: { color: K.navy } } }]]
    s.addTable(tbl, { x: M, y: 2.3, w: 6.3, colW: [5.1, 1.2], rowH: 0.48, fontFace: BODY, fontSize: 13, color: K.body, valign: 'middle', border: { type: 'solid', color: K.line, pt: 0.75 }, margin: [0.04, 0.14, 0.04, 0.14] })
    const rx = 7.35, rw = W - M - rx
    T(s, 'Источники финансирования', { x: rx, y: 2.3, w: rw, h: 0.4, fontSize: 16, bold: true, color: K.ink })
    const fin = [
      ['1', 'Резидентство ИТ-парка', 'Офис, бухгалтерия, юрист, сервер, трекинг: 3 000 ₽ в месяц или без оплаты'],
      ['2', '«Студенческий стартап» ФСИ', '1 млн ₽ на 12 месяцев без софинансирования; в 2026 году 12,6 тыс. заявок и 2,2 тыс. победителей'],
      ['3', '«Старт-1» ФСИ', 'До 5 млн ₽ на год без софинансирования для компании младше двух лет'],
      ['4', 'Подписки организаций', 'С III квартала 2027 года'],
    ]
    fin.forEach(([n, h, d], i) => {
      const y = 2.85 + i * 0.92
      circle(s, rx, y, 0.42, i === 3 ? K.wine : K.navy)
      T(s, n, { x: rx, y, w: 0.42, h: 0.42, fontSize: 14, bold: true, color: K.white, align: 'center', valign: 'middle' })
      T(s, h, { x: rx + 0.6, y: y - 0.03, w: rw - 0.6, h: 0.32, fontSize: 14, bold: true, color: K.ink })
      T(s, d, { x: rx + 0.6, y: y + 0.29, w: rw - 0.6, h: 0.58, fontSize: 12, color: K.soft })
    })
    source(s, 'Источники: страница Бизнес-инкубатора ИТ-парка (itpark.tech); Фонд содействия инновациям, условия конкурсов «Студенческий стартап» и «Старт» 2026 года (fasie.ru)')
    s.addNotes('Финансы. Первый год нам нужен примерно один миллион рублей: хостинг, выплаты команде, интервью и пилоты, маркетинг и юридические документы. Источники — резидентство ИТ-парка, грант «Студенческий стартап», затем «Старт-1» и собственная выручка от подписок.')
  }

  // ================= 15. КОМАНДА =================
  pres.addSection({ title: 'Команда и запрос' })
  {
    const s = slide('Команда и запрос')
    chip(s, 'Команда')
    title(s, 'Наука и разработка в одной команде')
    const people = [
      { img: fs.existsSync(A('marat.jpg')) ? 'marat.jpg' : null, ini: 'ММ', name: 'Мухамедьяров\nМарат Александрович', role: 'Эксперт науки и образования', c: K.wine, items: [
        'Доктор медицинских наук, профессор',
        'Заведующий кафедрой нормальной физиологии и директор Института нейронаук Казанского ГМУ',
        'Магистр ВШЭ «Управление в высшем образовании»',
        'Более 350 научных трудов, h-index 18 (Scopus), лауреат премии им. В. Зворыкина',
        'Председатель Татарстанского отделения Физиологического общества им. И. П. Павлова',
      ] },
      { img: 'azamat.jpg', ini: 'АС', name: 'Сибгатуллин\nАзамат Ильдарович', role: 'Разработчик', c: K.navy, items: [
        'Спроектировал и написал всю платформу: сервер на Go, сайт на React, базу данных, безопасность',
        'Довёл продукт до готовности к запуску: 40 этапов, тесты, аудит безопасности, нагрузочные проверки',
        'Студент Казанского ГМУ: сам из целевой аудитории',
      ] },
    ]
    const cw = (W - 2 * M - 0.35) / 2
    people.forEach((p, i) => {
      const x = M + i * (cw + 0.35), y = 1.85, h = 4.72
      card(s, x, y, cw, h, K.wash)
      const pw = 1.75, ph = 2.15
      if (p.img) s.addImage({ path: A(p.img), x: x + 0.3, y: y + 0.3, w: pw, h: ph, sizing: { type: 'cover', w: pw, h: ph } })
      else { s.addShape(pres.shapes.RECTANGLE, { x: x + 0.3, y: y + 0.3, w: pw, h: ph, fill: { color: K.blueW }, line: { type: 'none' } }); T(s, p.ini, { x: x + 0.3, y: y + 0.3, w: pw, h: ph, fontFace: HEAD, fontSize: 40, bold: true, color: K.navy, align: 'center', valign: 'middle' }) }
      T(s, p.name, { x: x + 2.3, y: y + 0.35, w: cw - 2.55, h: 0.95, fontFace: HEAD, fontSize: 20, bold: true, color: K.ink })
      T(s, p.role.toUpperCase(), { x: x + 2.3, y: y + 1.35, w: cw - 2.55, h: 0.3, fontSize: 11, bold: true, color: p.c, charSpacing: 1 })
      bullets(s, p.items, { x: x + 0.3, y: y + 2.62, w: cw - 0.55, h: 2.0, fontSize: 12.5, paraSpaceAfter: 2 })
    })
    T(s, [{ text: 'Ищем в инкубаторе: ', options: { bold: true, color: K.wine } }, { text: 'специалиста по продажам и маркетингу для работы с вузами и R&D-компаниями.' }], { x: M, y: 6.66, w: W - 2 * M, h: 0.3, fontSize: 12.5, color: K.body })
    s.addNotes('Команда. Марат Александрович — доктор медицинских наук, профессор, заведующий кафедрой и директор Института нейронаук КГМУ; он сам нанимает людей в науку и знает, как устроены вузы. Азамат — разработчик, который построил всю платформу. Нам не хватает человека на продажи и маркетинг, его мы надеемся найти в экосистеме ИТ-парка.')
  }

  // ================= 16. ЗАПРОС =================
  {
    const s = slide('Команда и запрос')
    chip(s, 'Запрос к инкубатору')
    title(s, 'Что нам нужно от ИТ-парка и что покажем через полгода')
    const asks = [
      ['LuCompass', 'Трекинг по Customer Development: найти модель заработка'],
      ['LuHandshake', 'Выход на вузы, НИИ и R&D-компании Татарстана для пилотов'],
      ['LuCalculator', 'Бюджет развития и выбор источника финансирования: гранты, инвесторы'],
      ['LuMegaphone', 'Маркетинг и продвижение в научном и ИТ-сообществе'],
      ['LuScale', 'Юридическое и бухгалтерское сопровождение, выбор формы юр. лица'],
      ['LuMonitor', 'Рабочие места и сервер для пилота'],
    ]
    T(s, 'Просим', { x: M, y: 1.9, w: 6, h: 0.4, fontSize: 16, bold: true, color: K.ink })
    for (let i = 0; i < asks.length; i++) {
      const y = 2.42 + i * 0.68
      await iconCircle(s, asks[i][0], M, y, 0.48, K.navy)
      T(s, asks[i][1], { x: M + 0.68, y, w: 6.2, h: 0.48, fontSize: 14, color: K.body, valign: 'middle' })
    }
    const rx = 7.75, rw = W - M - rx
    card(s, rx, 1.9, rw, 4.7, K.navyD)
    T(s, 'Через 6 месяцев покажем', { x: rx + 0.4, y: 2.15, w: rw - 0.8, h: 0.45, fontFace: HEAD, fontSize: 20, bold: true, color: K.white })
    const kpi = [['20+', 'интервью с организациями и соискателями'], ['3–5', 'пилотных организаций'], ['300+', 'профилей учёных'], ['1', 'подтверждённая модель оплаты и заявка на грант ФСИ']]
    kpi.forEach(([n, d], i) => {
      const y = 2.85 + i * 0.9
      T(s, n, { x: rx + 0.4, y, w: 1.35, h: 0.6, fontFace: HEAD, fontSize: 28, bold: true, color: 'E3B9BE', valign: 'middle' })
      T(s, d, { x: rx + 1.8, y, w: rw - 2.2, h: 0.6, fontSize: 13.5, color: K.white, valign: 'middle' })
    })
    s.addNotes('Наш запрос к инкубатору: трекинг, чтобы найти модель заработка, выход на вузы и компании Татарстана, помощь с бюджетом и грантами, маркетинг и юридическое сопровождение. Через полгода покажем результаты интервью, пилоты, первые 300 профилей и подтверждённую модель оплаты.')
  }

  // ================= 17. ФИНАЛ =================
  {
    const s = slide('Команда и запрос', 'DARK')
    const lh = 0.95
    s.addImage({ path: A('logo-white.png'), x: M, y: 0.7, w: logoW(lh), h: lh, altText: 'Логотип ИОН' })
    T(s, 'Помогаем учёным, преподавателям и инноваторам найти своё место, а вузам, НИИ и компаниям — своих людей', { x: M, y: 2.35, w: 10.5, h: 1.6, fontFace: HEAD, fontSize: 32, bold: true, color: K.white })
    ;['I', 'O', 'N'].forEach((k, i) => {
      const x = M + i * 2.75, y = 4.4
      circle(s, x, y, 0.55, SPH[k].color, 'FFFFFF')
      T(s, SPH[k].letter, { x, y, w: 0.55, h: 0.55, fontFace: HEAD, fontSize: 18, bold: true, color: K.white, align: 'center', valign: 'middle' })
      T(s, SPH[k].word, { x: x + 0.7, y, w: 2.0, h: 0.55, fontSize: 16, color: K.white, valign: 'middle' })
    })
    T(s, 'Спасибо! Готовы ответить на вопросы', { x: M, y: 5.6, w: 8, h: 0.4, fontSize: 16, color: K.onInkSoft })
    T(s, 'Марат Мухамедьяров · Азамат Сибгатуллин · Казань, 2026', { x: M, y: 6.75, w: 9, h: 0.3, fontSize: 11, color: K.onInkSoft })
    s.addNotes('ИОН помогает учёным, преподавателям и инноваторам найти своё место, а организациям — своих людей. Спасибо, готовы ответить на вопросы.')
  }

  // ================= ПРИЛОЖЕНИЕ =================
  pres.addSection({ title: 'Приложение' })
  {
    const s = slide('Приложение')
    chip(s, 'Приложение · вопросы экспертов', M, 0.46, K.wash2, K.soft)
    title(s, 'Главные риски и как мы их снимаем')
    const rows = [
      ['Нет вакансий — нет соискателей, и наоборот', 'Начинаем с организаций через сеть команды: конкурсы и набор в аспирантуру вузы проводят в любом случае, ИОН — дополнительный канал без лишней работы'],
      ['Вузы платят медленно и мало', 'Проверяем, кто платит и как удобно оплатить; первые пилоты без оплаты; запасные модели: продвижение аспирантуры и проекты R&D-компаний'],
      ['Учёные не придут на ещё один сайт', 'Профиль полезен сам по себе: резюме PDF, квартили журналов, подборка вакансий и напоминания о сроках'],
      ['Фальшивые организации и спам', 'Ручная модерация организаций спроектирована; уже есть ограничения частоты, жалобы в переписке, журнал действий'],
      ['Персональные данные', '152-ФЗ: согласие при регистрации, хостинг в России, видимость профиля выбирает человек'],
    ]
    const tbl = [[{ text: 'Риск', options: { bold: true, fill: { color: K.wash2 } } }, { text: 'Что делаем', options: { bold: true, fill: { color: K.wash2 } } }], ...rows.map(([a, b]) => [{ text: a, options: { bold: true, color: K.ink } }, { text: b }])]
    s.addTable(tbl, { x: M, y: 1.9, w: W - 2 * M, colW: [3.6, W - 2 * M - 3.6], fontFace: BODY, fontSize: 13, color: K.body, valign: 'middle', rowH: [0.45, 0.8, 0.8, 0.8, 0.8, 0.8], border: { type: 'solid', color: K.line, pt: 0.75 }, margin: [0.06, 0.15, 0.06, 0.15] })
    s.addNotes('Слайд для ответов на вопросы экспертов о рисках.')
  }
  {
    const s = slide('Приложение')
    chip(s, 'Приложение · технологии', M, 0.46, K.wash2, K.soft)
    title(s, 'Архитектура рассчитана на всю науку России')
    const box = (x, y, w, h, t, d, fill = K.wash, c = K.ink) => { card(s, x, y, w, h, fill); T(s, t, { x: x + 0.2, y: y + 0.15, w: w - 0.4, h: 0.35, fontSize: 14, bold: true, color: c }); if (d) T(s, d, { x: x + 0.2, y: y + 0.5, w: w - 0.4, h: h - 0.6, fontSize: 11.5, color: fill === K.navyD ? K.onInkSoft : K.soft }) }
    const arrow = (x, y, w) => s.addShape(pres.shapes.LINE, { x, y, w, h: 0, line: { color: K.edge, width: 1.5, endArrowType: 'triangle' } })
    box(M, 2.1, 1.9, 1.3, 'Браузер, телефон', 'один сайт для компьютера и телефона')
    arrow(M + 1.95, 2.75, 0.4)
    box(M + 2.4, 2.1, 2.0, 1.3, 'Сайт', 'React, TypeScript; тексты готовы к переводу')
    arrow(M + 4.45, 2.75, 0.4)
    box(M + 4.9, 2.1, 2.2, 1.3, 'Сервер', 'Go; несколько копий; без состояния в памяти', K.navyD, K.white)
    arrow(M + 7.15, 2.75, 0.4)
    box(M + 7.6, 2.1, 1.9, 1.3, 'PostgreSQL', 'единственный источник правды')
    box(M + 4.9, 3.75, 2.2, 1.25, 'Воркер', 'письма, подбор, напоминания, уборка')
    box(M + 2.4, 3.75, 2.2, 1.25, 'Файлы', 'хранилище S3 и антивирус ClamAV')
    box(M + 7.6, 3.75, 1.9, 1.25, 'Мониторинг', 'Prometheus, Grafana, GlitchTip')
    const rx = M + 9.85, rw = W - M - rx
    T(s, 'Безопасность', { x: rx, y: 2.1, w: rw, h: 0.35, fontSize: 15, bold: true, color: K.ink })
    bullets(s, ['Пароли argon2id, ссылки из писем одноразовые', 'Ограничение частоты запросов общее для всех копий', 'Каждая пара «роль × действие» проверена тестом', 'Линтер, govulncheck, semgrep, OWASP ZAP'], { x: rx, y: 2.5, w: rw, h: 3.2, fontSize: 12 })
    T(s, 'Цели нагрузки: 350 тыс. человек, 10 тыс. организаций, 300 тыс. вакансий, 1 млн откликов, ответ на поиск за 200 мс в 95 % случаев.', { x: M, y: 5.35, w: 9.4, h: 0.6, fontSize: 13, color: K.body })
    source(s, 'docs/ARCHITECTURE.md, docs/SCALING.md, docs/SECURITY.md в репозитории проекта')
    s.addNotes('Слайд для технических вопросов.')
  }
  {
    const s = slide('Приложение')
    chip(s, 'Приложение · источники', M, 0.46, K.wash2, K.soft)
    title(s, 'Источники цифр')
    const src = [
      'ИСИЭЗ НИУ ВШЭ. Кадры крупных и средних организаций науки: итоги 2024 года (2025). issek.hse.ru/news/1096744741.html',
      'ИСИЭЗ НИУ ВШЭ. Позитивные тенденции в аспирантуре закрепляются (2025). issek.hse.ru/news/1061520670.html',
      'ИСИЭЗ НИУ ВШЭ. Индикаторы науки: 2026; число организаций, выполнявших исследования и разработки, в 2024 году — более 4,1 тыс.',
      'Росстат. Российский статистический ежегодник 2025: численность профессорско-преподавательского персонала, 2024/2025 учебный год',
      'Минобрнауки России. Доклад о состоянии системы высшего образования, 2023: 724 организации и 524 филиала',
      'Приказ Минобрнауки России от 05.08.2021 № 715: конкурсы на замещение должностей научных работников, портал ученые-исследователи.рф',
      'Указ Президента РФ от 25.04.2022 № 231 «Об объявлении в Российской Федерации Десятилетия науки и технологий»',
      'МКПАО «Хэдхантер»: финансовые результаты за 2024 и 2025 годы (выручка, клиенты, средний чек крупных клиентов)',
      'Фонд содействия инновациям: «Студенческий стартап» 2026 (1 млн ₽), «Старт-1» (до 5 млн ₽). fasie.ru',
      'ИТ-парк, Казань: осенний отбор стартапов 2026, страница Бизнес-инкубатора. itpark.tech',
    ]
    s.addText(src.map((t, i) => ({ text: t, options: { bullet: { type: 'number' }, breakLine: i < src.length - 1, paraSpaceAfter: 6 } })), { isTextBox: true, x: M, y: 1.9, w: W - 2 * M, h: 4.8, fontFace: BODY, fontSize: 13, color: K.body, margin: 0, valign: 'top' })
    s.addNotes('Список источников всех цифр презентации.')
  }

  await pres.writeFile({ fileName: OUT })
  // Цвета бренда в теме файла (палитра PowerPoint). Скрипт из скилла pptx; без него презентация та же, меняется только палитра.
  const themeScript = process.env.PPTX_SKILL_DIR && path.join(process.env.PPTX_SKILL_DIR, 'scripts', 'apply_theme.js')
  if (themeScript && fs.existsSync(themeScript)) await require(themeScript).applyTheme(OUT, THEME)
  else console.warn('PPTX_SKILL_DIR не задан: тема бренда не записана в палитру файла')
  console.log('written', OUT)
}

main().catch((e) => { console.error(e); process.exit(1) })
