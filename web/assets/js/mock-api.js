/*
 * mock-api.js — временные ответы для методов, которых ещё нет на бэкенде.
 * Подключайте сразу после api.js:
 *   <script src="/assets/js/api.js"></script>
 *   <script src="/assets/js/mock-api.js"></script>
 *
 * Как это работает: файл оборачивает window.api. Запросы из MOCKS получают ответ отсюда,
 * все остальные (вход, /me/bootstrap, /applications) уходят на настоящий сервер.
 * Экраны не знают, мок это или сервер: ответы повторяют форму будущего API.
 *
 * Когда бэкенд сделает метод, поставьте ему false в MOCKS — и всё. Чтобы отключить все моки,
 * удалите подключение этого файла со страницы.
 *
 * Предлагаемые бэкенду ответы (их форма — это и есть ТЗ):
 *   GET /me        — { user: {...}, company: {...} }        профиль пользователя и компании
 *   GET /loans     — { loans: [...], other_banks: [...] }    кредиты в нашем банке с графиком и долги в других банках
 *   GET /me/offer  — { preapproved_limit, rate_from, ... }   предодобренный лимит для баннера
 *
 * Данные синтетические и зависят от ИНН демо-компании. Долг в других банках считается как
 * «общий долг из БКИ (/me/bootstrap) минус остаток по нашим кредитам», чтобы цифры сходились.
 */
(function () {
  'use strict';

  var MOCKS = {
    'GET /me': true,
    'GET /loans': true,
    'GET /me/offer': true,
  };

  var DELAY_MS = 250; // имитация сети, чтобы были видны состояния загрузки

  var realApi = window.api;

  // ---------- Демо-данные по компаниям ----------
  // Поля, которые есть в БД (название, ОГРН, ОКВЭД, руководитель, адрес), совпадают с сидом миграций.
  // Остальное (должность, счёт, кредиты) придумано для демонстрации.

  var COMPANIES = {
    '7700000001': {
      user: { full_name: 'Петров Пётр Петрович', phone: '+79990000001', email: 'client1@example.test', position: 'Генеральный директор' },
      company: {
        name: 'ООО «Демо Торг»', org_type: 'OOO', ogrn: '1027700000001', okved: '47.19',
        okved_title: 'Прочая розничная торговля в неспециализированных магазинах',
        registration_date: '2016-03-14', director: 'Петров Пётр Петрович',
        address: 'г. Москва, ул. Демонстрационная, д. 1', account_number: '40702810900000000001',
      },
      loans: [
        { id: 'L-1001', product_id: 'working_capital', product_name: 'Кредит на пополнение оборотных средств',
          contract_number: '0ОС-2026/1187', amount: 2000000, rate: 19, term_months: 18, paid_payments: 7, pay_day: 15 },
        { id: 'L-1002', product_id: 'auto_loan', product_name: 'Автокредит для бизнеса',
          contract_number: '0АК-2025/0342', amount: 1500000, rate: 15.5, term_months: 36, paid_payments: 20, pay_day: 5 },
      ],
      other_banks: [{ bank: 'Сбербанк', kind: 'Кредит на оборотные средства' }],
    },
    '7700000002': {
      user: { full_name: 'Смирнова Ольга Ивановна', phone: '+79990000002', email: 'client2@example.test', position: 'Генеральный директор' },
      company: {
        name: 'ООО «Демо Строй»', org_type: 'OOO', ogrn: '1027700000002', okved: '41.20',
        okved_title: 'Строительство жилых и нежилых зданий',
        registration_date: '2022-09-01', director: 'Смирнова Ольга Ивановна',
        address: 'г. Москва, ул. Демонстрационная, д. 2', account_number: '40702810900000000002',
      },
      loans: [],
      other_banks: [{ bank: 'ВТБ', kind: 'Инвестиционный кредит' }],
    },
    '7700000003': {
      user: { full_name: 'Кузнецов Андрей Викторович', phone: '+79990000003', email: 'client3@example.test', position: 'Генеральный директор' },
      company: {
        name: 'ООО «Демо Логистик»', org_type: 'OOO', ogrn: '1027700000003', okved: '49.41',
        okved_title: 'Деятельность автомобильного грузового транспорта',
        registration_date: '2019-06-20', director: 'Кузнецов Андрей Викторович',
        address: 'г. Москва, ул. Демонстрационная, д. 3', account_number: '40702810900000000003',
      },
      loans: [
        { id: 'L-3001', product_id: 'equipment_loan', product_name: 'Кредит на оборудование',
          contract_number: '0ОБ-2024/2210', amount: 2400000, rate: 16.5, term_months: 48, paid_payments: 26, pay_day: 20 },
      ],
      other_banks: [{ bank: 'Т-Банк', kind: 'Овердрафт' }],
    },
    '7700000004': {
      user: { full_name: 'Попов Сергей Николаевич', phone: '+79990000004', email: 'client4@example.test', position: 'Генеральный директор' },
      company: {
        name: 'ООО «Демо Долг»', org_type: 'OOO', ogrn: '1027700000004', okved: '46.90',
        okved_title: 'Торговля оптовая неспециализированная',
        registration_date: '2012-11-05', director: 'Попов Сергей Николаевич',
        address: 'г. Москва, ул. Демонстрационная, д. 4', account_number: '40702810900000000004',
      },
      loans: [
        { id: 'L-4001', product_id: 'ooo_loan', product_name: 'Кредит для ООО',
          contract_number: '0КО-2026/0415', amount: 3000000, rate: 20, term_months: 18, paid_payments: 5, pay_day: 10, overdue: true },
        { id: 'L-4002', product_id: 'credit_line', product_name: 'Возобновляемая кредитная линия (ВКЛ)',
          contract_number: '0ВКЛ-2026/0098', amount: 1500000, rate: 17, term_months: 24, paid_payments: 8, pay_day: 25 },
      ],
      other_banks: [
        { bank: 'Сбербанк', kind: 'Кредит под залог недвижимости' },
        { bank: 'ВТБ', kind: 'Оборотный кредит' },
      ],
    },
    '770000000005': {
      user: { full_name: 'Иванов Иван Иванович', phone: '+79990000005', email: 'client5@example.test', position: 'Индивидуальный предприниматель' },
      company: {
        name: 'ИП Иванов Демо', org_type: 'IP', ogrn: '304770000000005', okved: '56.10',
        okved_title: 'Деятельность ресторанов и услуги по доставке продуктов питания',
        registration_date: '2020-02-10', director: 'Иванов Иван Иванович',
        address: 'г. Москва, ул. Демонстрационная, д. 5', account_number: '40802810900000000005',
      },
      loans: [
        { id: 'L-5001', product_id: 'express', product_name: 'Экспресс-кредит для бизнеса',
          contract_number: '0ЭК-2026/0731', amount: 600000, rate: 20.5, term_months: 12, paid_payments: 4, pay_day: 12 },
      ],
      other_banks: [{ bank: 'Совкомбанк', kind: 'Кредит для ИП' }],
    },
  };

  // ---------- Кто вошёл ----------
  // Токен не содержит ИНН, поэтому берём его из ответа /me/bootstrap: запоминаем, когда кабинет
  // его вызывает, а если ещё не вызывал — спрашиваем сами.

  var bootstrap = null; // последний успешный ответ /me/bootstrap
  var bootstrapPromise = null;

  function loadBootstrap() {
    if (bootstrap) return Promise.resolve({ ok: true, status: 200, data: bootstrap });
    if (!bootstrapPromise) {
      bootstrapPromise = realApi('/me/bootstrap', { method: 'POST' }).then(function (res) {
        bootstrapPromise = null;
        if (res.ok) bootstrap = res.data;
        return res;
      });
    }
    return bootstrapPromise;
  }

  // ---------- График платежей ----------

  function round(n) {
    return Math.round(n * 100) / 100;
  }

  function isoDate(d) {
    var m = String(d.getMonth() + 1).padStart(2, '0');
    var day = String(d.getDate()).padStart(2, '0');
    return d.getFullYear() + '-' + m + '-' + day;
  }

  // Дата платежа: тот же день месяца, со сдвигом на offset месяцев (31-е в коротком месяце → последний день)
  function shiftMonths(base, offset, payDay) {
    var d = new Date(base.getFullYear(), base.getMonth() + offset, 1);
    var last = new Date(d.getFullYear(), d.getMonth() + 1, 0).getDate();
    d.setDate(Math.min(payDay, last));
    return d;
  }

  // Ближайшая дата платежа: сегодня или позже; для просроченного кредита — последняя прошедшая.
  function anchorDate(payDay, overdue) {
    var today = new Date();
    today.setHours(0, 0, 0, 0);
    var d = shiftMonths(today, 0, payDay);
    if (overdue) {
      // Чтобы просрочка была заметной, платёж пропущен минимум 5 дней назад.
      var limit = new Date(today);
      limit.setDate(limit.getDate() - 5);
      while (d > limit) d = shiftMonths(d, -1, payDay);
    } else if (d < today) {
      d = shiftMonths(d, 1, payDay);
    }
    return d;
  }

  function buildLoan(spec) {
    var r = spec.rate / 100 / 12;
    var payment = round((spec.amount * r) / (1 - Math.pow(1 + r, -spec.term_months)));
    var anchor = anchorDate(spec.pay_day, spec.overdue);
    var nextNumber = spec.paid_payments + 1; // номер ближайшего (или просроченного) платежа

    var balance = spec.amount;
    var schedule = [];
    var paidTotal = 0;
    for (var n = 1; n <= spec.term_months; n++) {
      var interest = round(balance * r);
      var amount = n === spec.term_months ? round(balance + interest) : payment;
      var principal = round(amount - interest);
      balance = Math.max(0, round(balance - principal));
      var status = n < nextNumber ? 'PAID' : n === nextNumber ? (spec.overdue ? 'OVERDUE' : 'NEXT') : 'PLANNED';
      if (status === 'PAID') paidTotal += amount;
      schedule.push({
        number: n,
        date: isoDate(shiftMonths(anchor, n - nextNumber, spec.pay_day)),
        amount: amount,
        principal: principal,
        interest: interest,
        balance_after: balance,
        status: status,
      });
    }

    var next = schedule[nextNumber - 1];
    // Остаток основного долга — после последнего оплаченного платежа
    var principalLeft = spec.paid_payments ? schedule[spec.paid_payments - 1].balance_after : spec.amount;

    return {
      id: spec.id,
      product_id: spec.product_id,
      product_name: spec.product_name,
      contract_number: spec.contract_number,
      amount: spec.amount,
      rate: spec.rate,
      term_months: spec.term_months,
      issued_at: isoDate(shiftMonths(anchor, -nextNumber, spec.pay_day)),
      maturity_date: schedule[schedule.length - 1].date,
      status: spec.overdue ? 'OVERDUE' : 'ACTIVE',
      monthly_payment: payment,
      principal_left: Math.round(principalLeft),
      paid_total: Math.round(paidTotal),
      payments_made: spec.paid_payments,
      payments_total: spec.term_months,
      next_payment: next ? { date: next.date, amount: next.amount } : null,
      overdue_amount: spec.overdue && next ? next.amount : 0,
      schedule: schedule,
    };
  }

  // ---------- Обработчики ----------

  function respond(status, data) {
    return new Promise(function (resolve) {
      setTimeout(function () {
        resolve({ ok: status >= 200 && status < 300, status: status, data: data, headers: new Headers() });
      }, DELAY_MS);
    });
  }

  // Неизвестная компания или /me/bootstrap не ответил: повторяем его статус
  function withCompany(handler) {
    return loadBootstrap().then(function (res) {
      if (!res.ok) return res;
      var c = COMPANIES[res.data.inn];
      if (!c) return respond(404, { error: 'company not found' });
      return handler(c, res.data);
    });
  }

  var handlers = {
    'GET /me': function () {
      return withCompany(function (c, b) {
        var company = Object.assign({ inn: b.inn, status: 'ACTIVE' }, c.company);
        return respond(200, { user: c.user, company: company });
      });
    },

    'GET /loans': function () {
      return withCompany(function (c, b) {
        var loans = c.loans.map(buildLoan);
        var ours = loans.reduce(function (s, l) { return s + l.principal_left; }, 0);
        var rest = Math.max(0, (b.total_debt || 0) - ours);
        // Долг в других банках: первому 60% (если банков два), последнему — остаток
        var others = [];
        if (rest > 0) {
          var left = rest;
          c.other_banks.forEach(function (o, i) {
            var last = i === c.other_banks.length - 1;
            var debt = last ? left : Math.round((rest * 0.6) / 1000) * 1000;
            left -= debt;
            others.push({ bank: o.bank, kind: o.kind, debt: debt });
          });
        }
        return respond(200, { loans: loans, other_banks: others });
      });
    },

    // Лимит — грубая оценка: половина запаса до долговой нагрузки 2,0, кратно 100 тыс., не больше 30 млн.
    'GET /me/offer': function () {
      return withCompany(function (c, b) {
        if (b.equity == null || b.total_debt == null) return respond(200, { preapproved_limit: null, reason: 'no_data' });
        var room = (2 * b.equity - b.total_debt) / 2;
        var limit = Math.min(30000000, Math.floor(room / 100000) * 100000);
        if (limit < 300000) return respond(200, { preapproved_limit: null, reason: 'high_debt_load' });
        var valid = new Date();
        valid.setDate(valid.getDate() + 30);
        return respond(200, { preapproved_limit: limit, rate_from: 17, valid_until: isoDate(valid) });
      });
    },
  };

  // ---------- Обёртка над api() ----------

  function mockedApi(path, options) {
    var method = ((options && options.method) || 'GET').toUpperCase();
    var key = method + ' ' + path.split('?')[0];
    if (MOCKS[key] && handlers[key]) return handlers[key](path, options);

    var promise = realApi(path, options);
    if (key === 'POST /me/bootstrap') {
      promise.then(function (res) {
        if (res.ok) bootstrap = res.data;
      });
    }
    return promise;
  }

  mockedApi.newKey = realApi.newKey;
  mockedApi.isMocked = function (method, path) {
    return !!MOCKS[method.toUpperCase() + ' ' + path];
  };

  window.api = mockedApi;
})();
