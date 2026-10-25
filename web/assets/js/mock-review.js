/*
 * mock-review.js — заглушка рассмотрения заявок: скоринг, работа сотрудника банка и ответы клиента.
 * Подключайте после mock-api.js (на страницах кабинета клиента и кабинета сотрудника).
 *
 * Заявки на рассмотрении лежат в localStorage (ключ STORE_KEY), поэтому кабинет сотрудника и кабинет
 * клиента, открытые в двух вкладках одного браузера, видят одни и те же заявки: сотрудник запросил
 * документы — клиент сразу видит запрос. Изменения из другой вкладки приходят событием
 * 'demo-review-change' на window.
 *
 * Как устроено:
 *   - при первом запуске в очередь кладутся демо-заявки пяти демо-компаний (SEED);
 *   - когда клиент отправляет заявку (настоящий POST /applications/{id}/submit), её копия попадает сюда,
 *     и упрощённый скоринг решает: балл от 65 — одобрено сразу, ниже — на ручную проверку сотруднику;
 *   - для заявок из хранилища ответы GET/PATCH /applications/{id} и новые методы отдаёт эта заглушка.
 *
 * Методы (форма ответов — ТЗ для бэкенда, см. web/BACKEND_TASKS.md):
 *   сотрудник: GET /employee/me, GET /employee/applications, GET /employee/applications/{id},
 *              POST /employee/applications/{id}/take | postpone | decision | request-documents
 *   клиент:    GET /applications (+ заявки отсюда), GET /applications/{id}, PATCH /applications/{id},
 *              POST /applications/{id}/submit | accept | decline | documents | resubmit
 *
 * Сбросить демо: MockReview.reset() в консоли или кнопка внизу кабинета сотрудника.
 */
(function () {
  'use strict';

  var STORE_KEY = 'demo_review_v1';
  var DELAY_MS = 250;
  var HOUR = 3600 * 1000;

  var EMPLOYEE = {
    id: 'emp-1',
    full_name: 'Сидорова Анна Сергеевна',
    position: 'Андеррайтер МСБ',
    email: 'underwriter@example.test',
  };

  // Долг и капитал демо-компаний — те же, что отдаёт воркер (internal/worker/synthetic.go)
  var FINANCIALS = {
    '7700000001': { total_debt: 3000000, equity: 5000000 },
    '7700000002': { total_debt: 2400000, equity: 4000000 },
    '7700000003': { total_debt: 1800000, equity: 3000000 },
    '7700000004': { total_debt: 7000000, equity: 2000000 },
    '770000000005': { total_debt: 800000, equity: 2000000 },
  };

  // ---------- Хранилище ----------

  function load() {
    try {
      var raw = localStorage.getItem(STORE_KEY);
      if (raw) return JSON.parse(raw);
    } catch (e) {
      /* нет доступа к хранилищу — работаем с демо-данными в памяти */
    }
    return null;
  }

  var memory = null;

  function store() {
    if (!memory) memory = load() || seed();
    return memory;
  }

  function save() {
    try {
      localStorage.setItem(STORE_KEY, JSON.stringify(memory));
    } catch (e) {}
    window.dispatchEvent(new CustomEvent('demo-review-change'));
  }

  // Другая вкладка поменяла заявки — перечитываем и сообщаем странице
  window.addEventListener('storage', function (e) {
    if (e.key !== STORE_KEY) return;
    memory = load() || seed();
    window.dispatchEvent(new CustomEvent('demo-review-change'));
  });

  function now() {
    return new Date().toISOString();
  }

  function ago(hours) {
    return new Date(Date.now() - hours * HOUR).toISOString();
  }

  function today() {
    return window.MockData.isoDate(new Date());
  }

  function newId() {
    return 'demo-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
  }

  // ---------- Справочники ----------

  function productName(id) {
    var p = window.Catalog && window.Catalog.PRODUCTS[id];
    return p ? p.name : id;
  }

  // Ставка продукта — из калькулятора (loans.js), как договорились в ТЗ
  function productRate(id) {
    var front = window.Catalog && window.Catalog.frontId(id);
    var p = front && window.Loans && window.Loans.productById(front);
    return p ? p.rate : 18;
  }

  function companySnapshot(inn) {
    var c = window.MockData.companies[inn];
    var f = FINANCIALS[inn] || {};
    if (!c) return { inn: inn, name: 'Компания ' + inn, org_type: 'OOO' };
    return {
      inn: inn,
      name: c.company.name,
      org_type: c.company.org_type,
      ogrn: c.company.ogrn,
      okved: c.company.okved,
      okved_title: c.company.okved_title,
      registration_date: c.company.registration_date,
      director: c.company.director,
      address: c.company.address,
      total_debt: f.total_debt != null ? f.total_debt : null,
      equity: f.equity != null ? f.equity : null,
      debt_to_equity: f.equity ? Math.round((f.total_debt / f.equity) * 100) / 100 : null,
      has_overdue: c.loans.some(function (l) { return l.overdue; }),
      applicant: { full_name: c.user.full_name, position: c.user.position, phone: c.user.phone, email: c.user.email },
    };
  }

  // ---------- Упрощённый скоринг ----------
  // Не настоящая модель (она в docs/scoring.md): несколько понятных факторов, чтобы на демо были разные исходы.

  var THRESHOLDS = { approve: 65, counter: 45 };

  function fmtRatio(v) {
    return new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 1 }).format(v);
  }

  function score(app) {
    var c = app.company;
    var s = 72;
    var neg = [];
    var pos = [];

    if (c.equity > 0) {
      var after = (c.total_debt + app.amount) / c.equity;
      if (after <= 1) {
        s += 8;
        pos.push('Долговая нагрузка после кредита останется низкой: ' + fmtRatio(after));
      } else if (after <= 2) {
        s -= Math.round(8 * (after - 1));
        neg.push('Долг / капитал после кредита вырастет до ' + fmtRatio(after));
      } else {
        s -= Math.min(30, Math.round(10 + 8 * (after - 2)));
        neg.push('Долг превысит капитал в ' + fmtRatio(after) + ' раза');
      }
    } else {
      s -= 25;
      neg.push('Нет данных о капитале компании');
    }

    if (c.has_overdue) {
      s -= 18;
      neg.push('Есть просроченный платёж по действующему кредиту');
    } else {
      s += 4;
      pos.push('Нет просрочек по кредитам');
    }

    var months = c.registration_date ? UI.monthsSince(c.registration_date) : 0;
    if (months < 24) {
      s -= 6;
      neg.push('Бизнесу меньше двух лет');
    } else if (months >= 60) {
      s += 4;
      pos.push('Бизнес работает больше пяти лет');
    }

    if (c.equity && app.amount > 2 * c.equity) {
      s -= 8;
      neg.push('Сумма кредита больше двух собственных капиталов');
    }

    s = Math.max(5, Math.min(98, s));
    return {
      model_version: 'demo-1',
      score: s,
      outcome: s >= THRESHOLDS.approve ? 'APPROVED' : 'MANUAL_REVIEW',
      thresholds: THRESHOLDS,
      reasons_negative: neg.map(function (t) { return { text: t }; }),
      reasons_positive: pos.map(function (t) { return { text: t }; }),
    };
  }

  // ---------- События истории ----------
  // type: STATUS — смена статуса; TAKEN — взята в работу; POSTPONED — отложена (клиент не видит);
  // DOCS_REQUESTED, DOCS_PROVIDED, TERMS_PROPOSED. by: 'client' | id сотрудника | 'system'.

  function event(app, type, extra) {
    var e = Object.assign({ type: type, at: now(), by: 'system', by_name: null, comment: null, client_visible: true }, extra || {});
    app.history.push(e);
    app.updated_at = e.at;
    return e;
  }

  function moveStatus(app, to, extra) {
    var from = app.status;
    app.status = to;
    return event(app, 'STATUS', Object.assign({ from: from, to: to }, extra || {}));
  }

  function byEmployee(extra) {
    return Object.assign({ by: EMPLOYEE.id, by_name: EMPLOYEE.full_name }, extra || {});
  }

  // Путь «отправлена → проверка → скоринг → решение» одной пачкой событий
  function runScoring(app) {
    app.decision = score(app);
    app.submitted_at = app.submitted_at || now();
    moveStatus(app, 'DATA_CHECK');
    moveStatus(app, 'SCORING');
    if (app.decision.outcome === 'APPROVED') {
      app.decision.offer = { product_id: app.product_id, amount: app.amount, term_months: app.term_months, rate: productRate(app.product_id) };
      moveStatus(app, 'APPROVED', { comment: 'Одобрено автоматически по результатам скоринга' });
    } else {
      moveStatus(app, 'MANUAL_REVIEW', { comment: 'Заявка передана специалисту банка' });
      app.queued_at = now();
    }
  }

  // ---------- Демо-очередь ----------

  function seedApp(inn, opts) {
    var app = {
      id: opts.id,
      version: 1,
      company_inn: inn,
      company: companySnapshot(inn),
      product_id: opts.product_id,
      amount: opts.amount,
      term_months: opts.term_months,
      purpose: opts.purpose,
      form: opts.form,
      documents: (opts.documents || []).map(function (d) {
        return { id: newId(), kind: d[0], filename: d[1], status: 'UPLOADED', created_at: ago(opts.hours + 0.2) };
      }),
      status: 'SUBMITTED',
      history: [{ type: 'STATUS', from: 'DRAFT', to: 'SUBMITTED', at: ago(opts.hours + 0.1), by: 'client', by_name: null, comment: null, client_visible: true }],
      decision: null,
      assigned_to: null,
      postponed_at: null,
      queued_at: ago(opts.hours),
      created_at: ago(opts.hours + 1),
      submitted_at: ago(opts.hours + 0.1),
      updated_at: ago(opts.hours),
    };
    app.decision = Object.assign(score(app), opts.decision || {});
    app.decision.outcome = 'MANUAL_REVIEW';
    ['DATA_CHECK', 'SCORING', 'MANUAL_REVIEW'].forEach(function (to, i) {
      app.history.push({
        type: 'STATUS', from: app.status, to: to, at: ago(opts.hours - i * 0.01), by: 'system', by_name: null,
        comment: to === 'MANUAL_REVIEW' ? 'Заявка передана специалисту банка' : null, client_visible: true,
      });
      app.status = to;
    });
    (opts.after || []).forEach(function (fn) { fn(app); });
    return app;
  }

  function seed() {
    var applicant = function (inn, collateral) {
      var c = window.MockData.companies[inn];
      return { position: c.user.position, has_collateral: collateral };
    };
    var apps = [
      seedApp('7700000004', {
        id: 'demo-q1', hours: 5, product_id: 'ooo_loan', amount: 2000000, term_months: 12,
        purpose: 'Закупка товара под сезон: оплата поставщикам',
        form: { applicant: applicant('7700000004', false) },
        documents: [['passport', 'pasport_popov.pdf'], ['financial_statements', 'balans_2025.pdf']],
      }),
      seedApp('7700000002', {
        id: 'demo-q2', hours: 26, product_id: 'equipment_loan', amount: 4500000, term_months: 48,
        purpose: 'Покупка башенного крана для строительных объектов',
        form: {
          applicant: applicant('7700000002', true),
          collateral: { type: 'equipment', description: 'Башенный кран Liebherr 132 EC-H8, 2019 г.', value: 3600000 },
          supply: { supplier: 'ООО «СтройТехИмпорт»', contract_number: '47/25', contract_date: '2026-10-14', contract_details: '№ 47/25 от 14.10.2026' },
        },
        documents: [['passport', 'pasport_smirnova.pdf'], ['equipment_purchase_contract', 'dogovor_47-25.pdf']],
        decision: {
          reasons_negative: [{ text: 'Долг / капитал после кредита вырастет до 1,7' }, { text: 'Залог оценён в 80% суммы кредита' }],
          reasons_positive: [{ text: 'Нет просрочек по кредитам' }],
          score: 54,
        },
      }),
      seedApp('770000000005', {
        id: 'demo-q3', hours: 0.7, product_id: 'express', amount: 1200000, term_months: 12,
        purpose: 'Открытие второй точки: ремонт и оборудование кухни',
        form: { applicant: applicant('770000000005', false) },
        documents: [['passport', 'pasport_ivanov.jpg']],
        decision: {
          reasons_negative: [{ text: 'Сумма больше половины годовой выручки' }, { text: 'Не приложена выписка по расчётному счёту' }],
          reasons_positive: [{ text: 'Низкая долговая нагрузка: 0,4' }, { text: 'Нет просрочек по кредитам' }],
          score: 58,
        },
      }),
      seedApp('7700000003', {
        id: 'demo-q4', hours: 30, product_id: 'auto_loan', amount: 3200000, term_months: 36,
        purpose: 'Покупка седельного тягача для междугородних перевозок',
        form: {
          applicant: applicant('7700000003', true),
          collateral: { type: 'vehicle', description: 'Седельный тягач КАМАЗ 54901, 2025 г.', value: 3900000 },
          vehicle: { make: 'КАМАЗ', model: '54901', year: 2025, vin: 'XTC549015R2512345' },
        },
        documents: [['passport', 'pasport_kuznetsov.pdf']],
        decision: {
          reasons_negative: [{ text: 'Долг / капитал после кредита вырастет до 1,7' }],
          reasons_positive: [{ text: 'Бизнес работает больше пяти лет' }, { text: 'Нет просрочек по кредитам' }],
          score: 61,
        },
        after: [function (app) {
          // Сотрудник уже запрашивал ПТС, клиент дослал два часа назад
          app.assigned_to = { id: EMPLOYEE.id, full_name: EMPLOYEE.full_name };
          app.history.push({ type: 'TAKEN', at: ago(29), by: EMPLOYEE.id, by_name: EMPLOYEE.full_name, comment: null, client_visible: true });
          app.history.push({ type: 'STATUS', from: 'MANUAL_REVIEW', to: 'INFO_REQUESTED', at: ago(28.5), by: EMPLOYEE.id, by_name: EMPLOYEE.full_name,
            comment: 'Приложите, пожалуйста, ПТС или договор купли-продажи тягача', client_visible: true, documents: ['vehicle_registration'] });
          app.documents.push({ id: newId(), kind: 'vehicle_registration', filename: 'pts_kamaz.pdf', status: 'UPLOADED', created_at: ago(2.1) });
          app.history.push({ type: 'DOCS_PROVIDED', at: ago(2), by: 'client', by_name: null, comment: null, client_visible: true, documents: ['vehicle_registration'] });
          app.history.push({ type: 'STATUS', from: 'INFO_REQUESTED', to: 'MANUAL_REVIEW', at: ago(2), by: 'client', by_name: null, comment: 'Клиент дослал документы', client_visible: true });
          app.queued_at = ago(2);
          app.docs_provided = true;
        }],
      }),
      seedApp('7700000001', {
        id: 'demo-q5', hours: 50, product_id: 'marketplace_seller', amount: 1500000, term_months: 12,
        purpose: 'Закупка товара для продаж на Wildberries к распродажам',
        form: { applicant: applicant('7700000001', false), marketplace: { platform: 'Wildberries', revenue_share: 65 } },
        documents: [['passport', 'pasport_petrov.pdf']],
        decision: {
          reasons_negative: [{ text: 'Нет отчёта о продажах с площадки' }, { text: 'Доля выручки через маркетплейс выросла за 3 месяца' }],
          reasons_positive: [{ text: 'Нет просрочек по кредитам' }, { text: 'Бизнес работает больше пяти лет' }],
          score: 49,
        },
        after: [function (app) {
          app.assigned_to = { id: EMPLOYEE.id, full_name: EMPLOYEE.full_name };
          app.postponed_at = ago(20);
          app.history.push({ type: 'TAKEN', at: ago(21), by: EMPLOYEE.id, by_name: EMPLOYEE.full_name, comment: null, client_visible: true });
          app.history.push({ type: 'POSTPONED', at: ago(20), by: EMPLOYEE.id, by_name: EMPLOYEE.full_name, comment: 'Ждём выгрузку продаж с Wildberries от аналитиков', client_visible: false });
        }],
      }),
    ];
    var s = { version: 1, apps: {}, issued: {} };
    apps.forEach(function (a) { s.apps[a.id] = a; });
    memory = s;
    save();
    return s;
  }

  // ---------- Ответы ----------

  function respond(status, data) {
    return new Promise(function (resolve) {
      setTimeout(function () {
        resolve({ ok: status >= 200 && status < 300, status: status, data: JSON.parse(JSON.stringify(data)), headers: new Headers() });
      }, DELAY_MS);
    });
  }

  function fail(status, message) {
    return respond(status, { error: message });
  }

  // То, что видит клиент: без внутренних событий сотрудника и без служебных полей
  function forClient(app) {
    var copy = JSON.parse(JSON.stringify(app));
    copy.history = copy.history.filter(function (e) { return e.client_visible !== false; });
    copy.product_name = productName(app.product_id);
    if (copy.assigned_to) copy.assigned_to = { full_name: copy.assigned_to.full_name };
    delete copy.postponed_at;
    delete copy.company;
    return copy;
  }

  function summaryForClient(app) {
    var c = forClient(app);
    return {
      id: c.id, product_id: c.product_id, product_name: c.product_name, amount: c.amount, term_months: c.term_months,
      status: c.status, submitted_at: c.submitted_at, created_at: c.created_at,
      decision: c.decision ? { outcome: c.decision.outcome, offer: c.decision.offer || null } : null,
      requested_documents: c.status === 'INFO_REQUESTED' ? c.decision.requested_documents || [] : [],
      assigned_to: c.assigned_to || null,
    };
  }

  function summaryForEmployee(app) {
    var mine = app.assigned_to && app.assigned_to.id === EMPLOYEE.id;
    return {
      id: app.id,
      company: { inn: app.company.inn, name: app.company.name, org_type: app.company.org_type },
      product_id: app.product_id, product_name: productName(app.product_id),
      amount: app.amount, term_months: app.term_months, status: app.status,
      score: app.decision ? app.decision.score : null,
      queued_at: app.queued_at, submitted_at: app.submitted_at, updated_at: app.updated_at,
      assigned_to: app.assigned_to ? { id: app.assigned_to.id, full_name: app.assigned_to.full_name } : null,
      assigned_to_me: !!mine,
      postponed_at: app.postponed_at, docs_provided: !!app.docs_provided,
      decision_outcome: app.decision ? app.decision.outcome : null,
    };
  }

  function currentInn() {
    return window.MockData.bootstrap().then(function (res) {
      return res.ok ? res.data.inn : null;
    });
  }

  function find(id) {
    return store().apps[id] || null;
  }

  // ---------- Сотрудник ----------

  var employee = {
    'GET /employee/me': function () {
      return respond(200, EMPLOYEE);
    },

    // Заявки, которые прошли или проходят через ручную проверку
    'GET /employee/applications': function () {
      var items = Object.keys(store().apps)
        .map(function (id) { return store().apps[id]; })
        .filter(function (a) {
          return a.history.some(function (e) { return e.to === 'MANUAL_REVIEW'; });
        })
        .map(summaryForEmployee);
      return respond(200, { items: items });
    },

    'GET /employee/applications/{id}': function (app) {
      var copy = JSON.parse(JSON.stringify(app));
      copy.product_name = productName(app.product_id);
      return respond(200, copy);
    },

    // Открыл заявку — взял в работу. Повторное открытие отложенной возвращает её в очередь.
    'POST /employee/applications/{id}/take': function (app) {
      if (app.status !== 'MANUAL_REVIEW') return fail(409, 'application is not in manual review');
      if (app.assigned_to && app.assigned_to.id !== EMPLOYEE.id) return fail(409, 'application is taken by another employee');
      if (!app.assigned_to) {
        app.assigned_to = { id: EMPLOYEE.id, full_name: EMPLOYEE.full_name };
        event(app, 'TAKEN', byEmployee());
      }
      if (app.postponed_at) {
        app.postponed_at = null;
        event(app, 'RESUMED', byEmployee({ client_visible: false }));
      }
      app.docs_provided = false;
      save();
      return employee['GET /employee/applications/{id}'](app);
    },

    'POST /employee/applications/{id}/postpone': function (app, body) {
      if (app.status !== 'MANUAL_REVIEW') return fail(409, 'application is not in manual review');
      app.postponed_at = now();
      event(app, 'POSTPONED', byEmployee({ comment: (body && body.comment) || null, client_visible: false }));
      save();
      return employee['GET /employee/applications/{id}'](app);
    },

    // outcome: APPROVED | REJECTED (нужна причина) | COUNTER_OFFER (нужен offer)
    'POST /employee/applications/{id}/decision': function (app, body) {
      body = body || {};
      if (app.status !== 'MANUAL_REVIEW') return fail(409, 'application is not in manual review');
      var comment = (body.comment || '').trim() || null;
      if (body.outcome === 'REJECTED' && !comment) return fail(400, 'comment is required for rejection');
      if (body.outcome === 'COUNTER_OFFER') {
        var o = body.offer || {};
        if (!o.product_id || !(o.amount > 0) || !(o.term_months > 0)) return fail(400, 'offer is incomplete');
        app.decision.offer = { product_id: o.product_id, amount: o.amount, term_months: o.term_months, rate: o.rate || productRate(o.product_id) };
      } else if (body.outcome === 'APPROVED') {
        app.decision.offer = { product_id: app.product_id, amount: app.amount, term_months: app.term_months, rate: productRate(app.product_id) };
      } else if (body.outcome !== 'REJECTED') {
        return fail(400, 'unknown outcome');
      }
      app.decision.outcome = body.outcome;
      app.decision.comment = comment;
      app.decision.decided_by = EMPLOYEE.full_name;
      app.postponed_at = null;
      moveStatus(app, body.outcome, byEmployee({ comment: comment, offer: app.decision.offer || null }));
      save();
      return employee['GET /employee/applications/{id}'](app);
    },

    'POST /employee/applications/{id}/request-documents': function (app, body) {
      body = body || {};
      if (app.status !== 'MANUAL_REVIEW') return fail(409, 'application is not in manual review');
      if (!body.documents || !body.documents.length) return fail(400, 'documents are required');
      app.decision.requested_documents = body.documents.slice();
      app.decision.request_comment = (body.comment || '').trim() || null;
      app.postponed_at = null;
      moveStatus(app, 'INFO_REQUESTED', byEmployee({ comment: app.decision.request_comment, documents: body.documents.slice() }));
      save();
      return employee['GET /employee/applications/{id}'](app);
    },
  };

  // ---------- Клиент ----------

  var client = {
    'GET /applications/{id}': function (app) {
      return respond(200, forClient(app));
    },

    // Черновик после отказа от встречного предложения клиент правит в мастере
    'PATCH /applications/{id}': function (app, body) {
      if (app.status !== 'DRAFT') return fail(409, 'application is not a draft');
      ['product_id', 'amount', 'term_months', 'purpose', 'form'].forEach(function (k) {
        if (body && body[k] !== undefined) app[k] = body[k];
      });
      app.version += 1;
      app.updated_at = now();
      save();
      return respond(200, forClient(app));
    },

    'POST /applications/{id}/submit': function (app) {
      if (app.status !== 'DRAFT') return fail(409, 'application is not a draft');
      moveStatus(app, 'SUBMITTED', { by: 'client' });
      app.assigned_to = null;
      app.submitted_at = now();
      runScoring(app);
      save();
      return respond(200, forClient(app));
    },

    // Клиент принял одобрение или встречное предложение: подписание и выдача сразу (демо)
    'POST /applications/{id}/accept': function (app) {
      if (app.status !== 'APPROVED' && app.status !== 'COUNTER_OFFER') return fail(409, 'application is not awaiting client decision');
      var offer = app.decision.offer || { product_id: app.product_id, amount: app.amount, term_months: app.term_months, rate: productRate(app.product_id) };
      app.product_id = offer.product_id;
      app.amount = offer.amount;
      app.term_months = offer.term_months;
      app.rate = offer.rate;
      app.issued_at = today();
      app.contract_number = '0' + String(Date.now()).slice(-6) + '/' + new Date().getFullYear();
      moveStatus(app, 'SIGNING', { by: 'client', comment: 'Клиент принял условия' });
      moveStatus(app, 'SIGNED', { by: 'client', comment: 'Договор подписан' });
      moveStatus(app, 'DISBURSED', { comment: 'Деньги перечислены на расчётный счёт' });
      moveStatus(app, 'ACTIVE');
      var issued = store().issued[app.company_inn] || (store().issued[app.company_inn] = []);
      issued.push({
        id: app.id, product_id: app.product_id, product_name: productName(app.product_id),
        contract_number: app.contract_number, amount: app.amount, rate: app.rate, term_months: app.term_months,
        paid_payments: 0, issued_at: app.issued_at,
      });
      save();
      return respond(200, forClient(app));
    },

    'POST /applications/{id}/decline': function (app) {
      if (app.status !== 'COUNTER_OFFER') return fail(409, 'application is not awaiting client decision');
      moveStatus(app, 'DRAFT', { by: 'client', comment: 'Клиент отказался от предложенных условий' });
      save();
      return respond(200, forClient(app));
    },

    // Настоящий метод — multipart (kind + file); в заглушке хватает имени файла: { kind, filename }
    'POST /applications/{id}/documents': function (app, body) {
      if (app.status !== 'INFO_REQUESTED' && app.status !== 'DRAFT') return fail(409, 'documents can not be added now');
      var doc = { id: newId(), kind: body.kind, filename: body.filename, status: 'UPLOADED', created_at: now() };
      app.documents.push(doc);
      app.updated_at = doc.created_at;
      save();
      return respond(201, doc);
    },

    // Клиент дослал запрошенное — заявка возвращается к тому же сотруднику, наверх очереди
    'POST /applications/{id}/resubmit': function (app) {
      if (app.status !== 'INFO_REQUESTED') return fail(409, 'no documents were requested');
      var need = app.decision.requested_documents || [];
      var have = app.documents.map(function (d) { return d.kind; });
      var missing = need.filter(function (k) { return have.indexOf(k) === -1; });
      if (missing.length) return respond(422, { missing_documents: missing });
      event(app, 'DOCS_PROVIDED', { by: 'client', documents: need });
      moveStatus(app, 'DATA_CHECK', { by: 'client', comment: 'Клиент дослал документы' });
      moveStatus(app, 'MANUAL_REVIEW', { comment: 'Заявка возвращена специалисту банка' });
      app.queued_at = now();
      app.postponed_at = null;
      app.docs_provided = true;
      save();
      return respond(200, forClient(app));
    },
  };

  // Настоящая заявка только что отправлена — копируем к себе и прогоняем скоринг
  function importSubmitted(real) {
    return currentInn().then(function (inn) {
      if (!inn) return real;
      var app = {
        id: real.id,
        version: real.version,
        company_inn: inn,
        company: companySnapshot(inn),
        product_id: real.product_id,
        amount: real.amount,
        term_months: real.term_months,
        purpose: real.purpose,
        form: real.form || {},
        documents: [],
        status: 'SUBMITTED',
        history: (real.history || []).map(function (h) {
          return { type: 'STATUS', from: h.from, to: h.to, at: h.at, by: 'client', by_name: null, comment: null, client_visible: true };
        }),
        decision: null,
        assigned_to: null,
        postponed_at: null,
        created_at: real.created_at,
        submitted_at: real.submitted_at || now(),
        updated_at: now(),
      };
      // Свежие долг и капитал клиента — из /me/bootstrap
      return window.MockData.bootstrap().then(function (b) {
        if (b.ok && b.data.equity != null) {
          app.company.total_debt = b.data.total_debt;
          app.company.equity = b.data.equity;
          app.company.debt_to_equity = b.data.debt_to_equity;
        }
        runScoring(app);
        store().apps[app.id] = app;
        save();
        return forClient(app);
      });
    });
  }

  // ---------- Обёртка над api() ----------

  var inner = window.api;

  function route(method, path) {
    var m = /^\/(employee\/)?applications\/([^/]+)(\/[a-z-]+)?$/.exec(path);
    if (!m) return null;
    return { key: method + ' /' + (m[1] || '') + 'applications/{id}' + (m[3] || ''), id: decodeURIComponent(m[2]) };
  }

  function wrapped(path, options) {
    var method = ((options && options.method) || 'GET').toUpperCase();
    var clean = path.split('?')[0];
    var body = options && options.json;
    var key = method + ' ' + clean;

    if (employee[key]) return employee[key](null, body);

    var r = route(method, clean);
    if (r) {
      var app = find(r.id);
      if (employee[r.key]) return app ? employee[r.key](app, body) : fail(404, 'application not found');
      if (app && client[r.key]) return client[r.key](app, body);

      // Настоящая заявка: после успешной отправки копируем её в демо-рассмотрение
      if (r.key === 'POST /applications/{id}/submit') {
        return inner(path, options).then(function (res) {
          if (!res.ok) return res;
          return importSubmitted(res.data).then(function (data) {
            return { ok: true, status: 200, data: data, headers: res.headers };
          });
        });
      }
      return inner(path, options);
    }

    // Список заявок клиента: настоящие + из демо-рассмотрения (они главнее)
    if (key === 'GET /applications') {
      return Promise.all([inner(path, options), currentInn()]).then(function (all) {
        var res = all[0];
        var inn = all[1];
        if (!res.ok || !inn) return res;
        var mine = Object.keys(store().apps)
          .map(function (id) { return store().apps[id]; })
          .filter(function (a) { return a.company_inn === inn; });
        var ids = mine.map(function (a) { return a.id; });
        var list = (res.data || []).filter(function (a) { return ids.indexOf(a.id) === -1; })
          .concat(mine.filter(function (a) { return a.status !== 'ACTIVE'; }).map(summaryForClient));
        return { ok: true, status: 200, data: list, headers: res.headers };
      });
    }

    return inner(path, options);
  }

  wrapped.newKey = inner.newKey;
  wrapped.isMocked = function (method, p) {
    return (inner.isMocked && inner.isMocked(method, p)) || /^\/employee\//.test(p);
  };
  window.api = wrapped;

  window.MockReview = {
    employee: EMPLOYEE,
    // Кредиты, выданные по принятым заявкам, — для GET /loans в mock-api.js
    issuedLoans: function (inn) {
      return JSON.parse(JSON.stringify(store().issued[inn] || []));
    },
    reset: function () {
      try {
        localStorage.removeItem(STORE_KEY);
      } catch (e) {}
      memory = null;
      store();
    },
  };
})();
