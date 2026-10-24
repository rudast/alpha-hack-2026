/*
 * loans.js — каталог кредитных продуктов, правила доступности и расчёт платежа.
 * Подключайте до calculator.js и на странице личного кабинета:
 *   <script src="/assets/js/loans.js"></script>
 *
 * Всё работает только на фронтенде: ничего не уходит на сервер и не пишется в БД.
 *
 * Что здесь есть:
 *   Loans.PRODUCTS                 — 14 продуктов из таблицы «кредитные продукты»
 *   Loans.getAvailable(query)      — только доступные продукты под введённые данные, с платежом и переплатой
 *   Loans.isAvailable(product, q)  — проверка одного продукта
 *   Loans.offer(product, q)        — ежемесячный платёж и переплата (аннуитет)
 *   LoanDraft.save / get / clear   — расчёт из калькулятора, который «переезжает» в личный кабинет
 *
 * query = { org: 'ooo' | 'ip', collateral: 'without' | 'with', age: 'gte9' | 'lt9', amount: ₽, term: месяцы }
 *
 * Как читать таблицу:
 *   «Залог»          нет → только «Без залога»; да → только «С залогом»; любой → оба варианта
 *   «≥9 мес.»        да → бизнес должен вести дела 9 месяцев и больше;
 *                    нет → доступен и молодому бизнесу (тогда действует «Лимит <9 мес.», если он указан)
 *   «Лимит <9 мес.»  максимальная сумма для бизнеса младше 9 месяцев; «—» → отдельного лимита нет
 *
 * ВАЖНО: в таблице нет процентных ставок. Ставки Селлеров (18,5%) и Экспресса (20,5%) взяты
 * из макета, остальные — ДЕМО-ЗНАЧЕНИЯ-плейсхолдеры. Замените поле rate на реальные ставки команды.
 * То же с описаниями и группировкой по категориям: это текст для демонстрации.
 */
(function (root) {
  'use strict';

  var TH = 1000;
  var MLN = 1000000;

  // Границы ползунков калькулятора
  var LIMITS = { amountMin: 50 * TH, amountMax: 30 * MLN, termMin: 1, termMax: 120 };

  var CATEGORIES = {
    cashflow: 'Оборотный кредит (cash flow)',
    line: 'Кредитная линия',
    invest: 'Инвестиционный кредит',
    secured: 'Кредит под залог',
    business: 'Покупка бизнеса',
  };

  // org: для кого продукт; collateral: 'without' | 'with' | 'any'; needs9: нужен ли бизнес от 9 месяцев;
  // term: [мин, макс] в месяцах; amount: [мин, макс] в рублях; youngMax: лимит суммы для бизнеса младше 9 месяцев
  var PRODUCTS = [
    {
      id: 'working-capital', num: 1, category: 'cashflow', icon: 'coins', rate: 19.0,
      title: 'Оборотные средства',
      desc: 'Пополнение оборотных средств: закупки, зарплаты, аренда и другие текущие расходы.',
      org: ['ooo', 'ip'], collateral: 'without', needs9: true,
      term: [3, 36], amount: [100 * TH, 30 * MLN], youngMax: null,
    },
    {
      id: 'ooo-unsecured', num: 2, category: 'cashflow', icon: 'briefcase', rate: 20.0,
      title: 'Кредит для ООО без залога',
      desc: 'Финансирование развития компании без обеспечения.',
      org: ['ooo'], collateral: 'without', needs9: true,
      term: [3, 36], amount: [300 * TH, 30 * MLN], youngMax: null,
    },
    {
      id: 'ip-unsecured', num: 3, category: 'cashflow', icon: 'briefcase', rate: 21.0,
      title: 'Кредит для ИП без залога',
      desc: 'Финансирование для индивидуальных предпринимателей без обеспечения.',
      org: ['ip'], collateral: 'without', needs9: true,
      term: [3, 36], amount: [50 * TH, 15 * MLN], youngMax: null,
    },
    {
      id: 'seller', num: 4, category: 'cashflow', icon: 'bag', rate: 18.5,
      title: 'Кредит для селлеров маркетплейсов',
      desc: 'Целевое финансирование под аналитику продаж на маркетплейсах для пополнения складских запасов.',
      org: ['ooo', 'ip'], collateral: 'without', needs9: false,
      term: [3, 18], amount: [50 * TH, 15 * MLN], youngMax: 3 * MLN,
    },
    {
      id: 'express', num: 5, category: 'cashflow', icon: 'rocket', rate: 20.5,
      title: 'Экспресс-кредит для бизнеса',
      desc: 'Ускоренное онлайн-рассмотрение по минимальному набору сведений. Доступен также молодым компаниям.',
      org: ['ooo', 'ip'], collateral: 'without', needs9: false,
      term: [1, 18], amount: [50 * TH, 5 * MLN], youngMax: 2 * MLN,
    },
    {
      id: 'small', num: 6, category: 'cashflow', icon: 'plus', rate: 22.0,
      title: 'Кредит до 1 млн ₽',
      desc: 'Небольшая сумма на текущие задачи бизнеса без залога.',
      org: ['ooo', 'ip'], collateral: 'without', needs9: false,
      term: [1, 24], amount: [50 * TH, 1 * MLN], youngMax: null,
    },
    {
      id: 'overdraft', num: 7, category: 'cashflow', icon: 'card', rate: 17.5,
      title: 'Овердрафт',
      desc: 'Короткое финансирование для покрытия кассовых разрывов.',
      org: ['ooo', 'ip'], collateral: 'without', needs9: true,
      term: [1, 12], amount: [50 * TH, 15 * MLN], youngMax: null,
    },
    {
      id: 'credit-line', num: 8, category: 'line', icon: 'cycle', rate: 17.0,
      title: 'Возобновляемая кредитная линия',
      desc: 'Лимит, которым можно пользоваться частями и пополнять по мере возврата. Подходит и с залогом, и без.',
      org: ['ooo', 'ip'], collateral: 'any', needs9: true,
      term: [3, 24], amount: [300 * TH, 30 * MLN], youngMax: null,
    },
    {
      id: 'equipment', num: 9, category: 'invest', icon: 'gear', rate: 16.5,
      title: 'Кредит на оборудование',
      desc: 'Покупка оборудования для производства и услуг под залог.',
      org: ['ooo', 'ip'], collateral: 'with', needs9: true,
      term: [12, 84], amount: [500 * TH, 30 * MLN], youngMax: null,
    },
    {
      id: 'auto', num: 10, category: 'invest', icon: 'car', rate: 15.5,
      title: 'Автокредит для бизнеса',
      desc: 'Покупка транспорта для компании: легкового, грузового или специального.',
      org: ['ooo', 'ip'], collateral: 'with', needs9: false,
      term: [12, 60], amount: [500 * TH, 20 * MLN], youngMax: 5 * MLN,
    },
    {
      id: 'secured-property', num: 11, category: 'secured', icon: 'house', rate: 15.0,
      title: 'Кредит под залог недвижимости или оборудования',
      desc: 'Крупные суммы на длительный срок под имеющееся имущество.',
      org: ['ooo', 'ip'], collateral: 'with', needs9: false,
      term: [6, 120], amount: [500 * TH, 30 * MLN], youngMax: 10 * MLN,
    },
    {
      id: 'mortgage-ooo', num: 12, category: 'secured', icon: 'house', rate: 14.5,
      title: 'Ипотека для ООО',
      desc: 'Покупка коммерческой недвижимости для компании.',
      org: ['ooo'], collateral: 'with', needs9: true,
      term: [24, 120], amount: [2 * MLN, 30 * MLN], youngMax: null,
    },
    {
      id: 'mortgage-ip', num: 13, category: 'secured', icon: 'house', rate: 14.5,
      title: 'Ипотека для ИП',
      desc: 'Покупка коммерческой недвижимости для индивидуального предпринимателя.',
      org: ['ip'], collateral: 'with', needs9: true,
      term: [24, 120], amount: [1500 * TH, 30 * MLN], youngMax: null,
    },
    {
      id: 'ready-business', num: 14, category: 'business', icon: 'factory', rate: 16.0,
      title: 'Покупка завода или готового бизнеса',
      desc: 'Финансирование покупки действующего производства или готового бизнеса.',
      org: ['ooo', 'ip'], collateral: 'with', needs9: true,
      term: [24, 84], amount: [5 * MLN, 30 * MLN], youngMax: null,
    },
  ];

  // ---------- Значения ползунков ----------
  // Сумма и срок выбираются не равномерно: мелкие значения нужны чаще, чем 30 млн, поэтому там шаг тоньше.

  function buildAmountStops() {
    var stops = [];
    var v;
    for (v = 50 * TH; v <= 1 * MLN; v += 25 * TH) stops.push(v); // 50 тыс. – 1 млн, шаг 25 тыс.
    for (v = 1100 * TH; v <= 5 * MLN; v += 100 * TH) stops.push(v); // до 5 млн, шаг 100 тыс.
    for (v = 5500 * TH; v <= 30 * MLN; v += 500 * TH) stops.push(v); // до 30 млн, шаг 500 тыс.
    return stops;
  }

  function buildTermStops() {
    var stops = [];
    var m;
    for (m = 1; m <= 12; m += 1) stops.push(m); // до года, каждый месяц
    for (m = 15; m <= 36; m += 3) stops.push(m); // до 3 лет, раз в квартал
    for (m = 42; m <= 120; m += 6) stops.push(m); // до 10 лет, раз в полгода
    return stops;
  }

  // ---------- Правила доступности ----------

  function productById(id) {
    for (var i = 0; i < PRODUCTS.length; i++) {
      if (PRODUCTS[i].id === id) return PRODUCTS[i];
    }
    return null;
  }

  // Максимальная сумма с учётом лимита для бизнеса младше 9 месяцев
  function maxAmountFor(product, age) {
    if (age === 'lt9' && product.youngMax) return Math.min(product.amount[1], product.youngMax);
    return product.amount[1];
  }

  function isAvailable(product, q) {
    if (product.org.indexOf(q.org) === -1) return false;
    if (product.collateral !== 'any' && product.collateral !== q.collateral) return false;
    if (product.needs9 && q.age !== 'gte9') return false;
    if (q.amount < product.amount[0] || q.amount > maxAmountFor(product, q.age)) return false;
    if (q.term < product.term[0] || q.term > product.term[1]) return false;
    return true;
  }

  // ---------- Расчёт ----------

  // Аннуитетный платёж: ставка в процентах годовых, срок в месяцах
  function annuity(amount, ratePercent, months) {
    var r = ratePercent / 100 / 12;
    if (r === 0) return amount / months;
    return (amount * r) / (1 - Math.pow(1 + r, -months));
  }

  function offer(product, q) {
    var payment = annuity(q.amount, product.rate, q.term);
    return {
      payment: Math.round(payment),
      overpayment: Math.round(payment * q.term - q.amount),
    };
  }

  // Доступные продукты по возрастанию ставки. Недоступные в список не попадают.
  function getAvailable(q) {
    return PRODUCTS.filter(function (p) {
      return isAvailable(p, q);
    })
      .map(function (p) {
        var o = offer(p, q);
        return { product: p, payment: o.payment, overpayment: o.overpayment };
      })
      .sort(function (a, b) {
        return a.product.rate - b.product.rate || a.product.num - b.product.num;
      });
  }

  // ---------- Форматирование ----------

  var numberFormat = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 0 });
  var decimalFormat = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 1 });
  var NBSP = '\u00a0';

  function plural(n, one, few, many) {
    var mod10 = n % 10;
    var mod100 = n % 100;
    if (mod10 === 1 && mod100 !== 11) return one;
    if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return few;
    return many;
  }

  var fmt = {
    number: function (n) {
      return numberFormat.format(n);
    },
    rub: function (n) {
      return numberFormat.format(n) + NBSP + '₽';
    },
    rate: function (r) {
      return decimalFormat.format(r) + '%';
    },
    // 50 тыс., 1,5 млн, 30 млн
    amountShort: function (n) {
      if (n >= MLN) return decimalFormat.format(n / MLN) + NBSP + 'млн';
      return decimalFormat.format(n / TH) + NBSP + 'тыс.';
    },
    amountRange: function (min, max) {
      return fmt.amountShort(min) + ' – ' + fmt.amountShort(max) + NBSP + '₽';
    },
    // 1 мес., 18 мес. → «1 год 6 мес.», 36 → «3 года»
    term: function (months) {
      var years = Math.floor(months / 12);
      var rest = months % 12;
      if (years === 0) return months + NBSP + 'мес.';
      var text = years + NBSP + plural(years, 'год', 'года', 'лет');
      return rest ? text + ' ' + rest + NBSP + 'мес.' : text;
    },
    termRange: function (min, max) {
      return min + '–' + max + NBSP + 'мес.';
    },
  };

  var LABELS = {
    org: { ooo: 'ООО', ip: 'ИП' },
    collateral: { without: 'без залога', with: 'с залогом' },
    age: { gte9: 'бизнес от 9 месяцев', lt9: 'бизнес младше 9 месяцев' },
  };

  function queryText(q) {
    return LABELS.org[q.org] + ', ' + LABELS.collateral[q.collateral] + ', ' + LABELS.age[q.age];
  }

  // ---------- Расчёт, переходящий в личный кабинет (только браузер) ----------
  // Хранится в sessionStorage: живёт до закрытия вкладки, на сервер не отправляется.

  var DRAFT_KEY = 'loan_draft';

  function storage() {
    try {
      return root.sessionStorage || null;
    } catch (e) {
      return null; // доступ к хранилищу запрещён настройками браузера
    }
  }

  function isValidDraft(d) {
    return (
      d &&
      d.v === 1 &&
      productById(d.productId) &&
      (d.org === 'ooo' || d.org === 'ip') &&
      (d.collateral === 'without' || d.collateral === 'with') &&
      (d.age === 'gte9' || d.age === 'lt9') &&
      isFinite(d.amount) &&
      isFinite(d.term)
    );
  }

  var LoanDraft = {
    save: function (d) {
      var s = storage();
      if (!s) return false;
      try {
        s.setItem(
          DRAFT_KEY,
          JSON.stringify({
            v: 1,
            productId: d.productId,
            org: d.org,
            collateral: d.collateral,
            age: d.age,
            amount: d.amount,
            term: d.term,
            savedAt: Date.now(),
          })
        );
        return true;
      } catch (e) {
        return false;
      }
    },

    get: function () {
      var s = storage();
      if (!s) return null;
      try {
        var raw = s.getItem(DRAFT_KEY);
        if (!raw) return null;
        var d = JSON.parse(raw);
        if (!isValidDraft(d)) {
          s.removeItem(DRAFT_KEY);
          return null;
        }
        return d;
      } catch (e) {
        return null;
      }
    },

    clear: function () {
      var s = storage();
      try {
        if (s) s.removeItem(DRAFT_KEY);
      } catch (e) {
        /* ничего страшного: расчёт просто останется до закрытия вкладки */
      }
    },
  };

  // Расшифровка сохранённого расчёта: продукт из каталога + платёж, посчитанный заново
  function fromDraft(d) {
    var product = d && productById(d.productId);
    if (!product) return null;
    return { product: product, query: d, offer: offer(product, d) };
  }

  var api = {
    LIMITS: LIMITS,
    CATEGORIES: CATEGORIES,
    PRODUCTS: PRODUCTS,
    AMOUNT_STOPS: buildAmountStops(),
    TERM_STOPS: buildTermStops(),
    productById: productById,
    maxAmountFor: maxAmountFor,
    isAvailable: isAvailable,
    getAvailable: getAvailable,
    annuity: annuity,
    offer: offer,
    fromDraft: fromDraft,
    queryText: queryText,
    fmt: fmt,
  };

  root.Loans = api;
  root.LoanDraft = LoanDraft;
  if (typeof module === 'object' && module.exports) {
    module.exports = { Loans: api, LoanDraft: LoanDraft };
  }
})(typeof window !== 'undefined' ? window : globalThis);
