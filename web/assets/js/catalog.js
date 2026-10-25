/*
 * catalog.js — каталог продуктов бэкенда, цели кредита для мастера заявки и статусы заявок.
 * Подключайте после loans.js: ставки и правила доступности калькулятора берутся оттуда.
 *
 * ВРЕМЕННО: группы полей, документы и сроки скопированы из internal/catalog/products.yaml,
 * потому что GET /products на бэкенде ещё нет. Когда метод появится, данные надо брать из него.
 *
 * У калькулятора (loans.js) и бэкенда разные идентификаторы продуктов. Заявка уходит на сервер
 * с id бэкенда, поэтому здесь есть таблица соответствия FRONT_TO_BACK.
 *
 * Что здесь есть:
 *   Catalog.GOALS                — цели кредита («на что нужны деньги») для первого шага мастера
 *   Catalog.backId(frontId)      — id продукта на бэкенде по id калькулятора; frontId(backId) — обратно
 *   Catalog.isAvailable(p, q)    — доступность по правилам калькулятора И бэкенда (сроки, суммы, ООО/ИП)
 *   Catalog.recommend(goal, q)   — подходящие продукты для цели, лучший первым
 *   Catalog.groupsFor(backId)    — группы полей анкеты продукта (базовые + свои)
 *   Catalog.documentsFor(backId) — документы: { required: [...], optional: [...] }
 *   Catalog.STATUS               — подписи и цвета статусов заявки
 */
(function () {
  'use strict';

  var Loans = window.Loans;

  // ---------- Копия products.yaml ----------

  var BASE_GROUPS = ['applicant'];
  var BASE_DOCUMENTS = ['passport', 'bki_consent'];
  var BASE_OPTIONAL_DOCUMENTS = ['egrul_extract'];

  var DOCUMENTS = {
    passport: 'Паспорт заявителя (скан 2–3 стр.)',
    bki_consent: 'Согласие на обработку персональных данных и запрос в БКИ',
    egrul_extract: 'Выписка из ЕГРЮЛ/ЕГРИП',
    account_statement: 'Выписка по расчётному счёту',
    financial_statements: 'Бухгалтерский баланс и отчёт о прибылях и убытках (или декларация УСН)',
    marketplace_sales_report: 'Отчёт о продажах с площадки маркетплейса',
    title_documents: 'Правоустанавливающие документы на залог',
    equipment_purchase_contract: 'Договор купли-продажи или счёт-фактура на оборудование',
    equipment_specification: 'Спецификация оборудования',
    vehicle_registration: 'ПТС/СТС или договор купли-продажи автомобиля',
    egrn_extract: 'Выписка из ЕГРН',
    technical_passport: 'Технический паспорт',
    valuation_report: 'Отчёт об оценке',
    business_plan: 'Бизнес-план',
    target_financial_statements: 'Финансовая отчётность приобретаемого бизнеса',
    business_purchase_contract: 'Договор купли-продажи предприятия или долей',
    preliminary_purchase_contract: 'Предварительный договор купли-продажи',
  };

  // Типы полей: text, number, money, bool, select, date. hint — подсказка под полем (только фронтенд).
  // date хранится как 'YYYY-MM-DD'; max: 'today' — дата не позже сегодняшней.
  var FIELD_GROUPS = {
    applicant: {
      title: 'Заявитель',
      fields: [
        { key: 'position', label: 'Должность в компании', type: 'text', required: true },
        { key: 'has_collateral', label: 'Есть залог', type: 'bool', required: true },
      ],
    },
    account: {
      title: 'Расчётный счёт',
      fields: [{ key: 'account_number', label: 'Номер расчётного счёта', type: 'text', required: true, hint: '20 цифр' }],
    },
    rko: {
      title: 'Расчётно-кассовое обслуживание',
      fields: [{ key: 'has_settlement_account', label: 'Счёт для бизнеса уже открыт в нашем банке', type: 'bool', required: true }],
    },
    credit_line: {
      title: 'Кредитная линия',
      fields: [{ key: 'desired_limit', label: 'Желаемый лимит линии', type: 'money', required: true }],
    },
    marketplace: {
      title: 'Маркетплейс',
      fields: [
        { key: 'platform', label: 'Площадка', type: 'select', required: true, options: ['Wildberries', 'Ozon', 'Яндекс Маркет', 'Другая'] },
        { key: 'revenue_share', label: 'Доля выручки через маркетплейс, %', type: 'number', required: true },
      ],
    },
    collateral: {
      title: 'Предмет залога',
      fields: [
        { key: 'type', label: 'Тип предмета залога', type: 'select', required: true, options: ['equipment', 'vehicle', 'real_estate', 'other'] },
        { key: 'description', label: 'Описание предмета залога', type: 'text', required: true },
        { key: 'value', label: 'Оценочная стоимость залога', type: 'money', required: true },
      ],
    },
    supply: {
      title: 'Поставка оборудования',
      fields: [
        { key: 'supplier', label: 'Поставщик', type: 'text', required: true },
        // На бэкенде это одно поле contract_details («№ 15/24 от 03.10.2026»), см. composeForm()
        { key: 'contract_number', label: 'Номер договора или счёта', type: 'text', required: true, hint: 'Например, 15/24' },
        { key: 'contract_date', label: 'Дата договора или счёта', type: 'date', required: true, max: 'today' },
      ],
    },
    vehicle: {
      title: 'Автомобиль',
      fields: [
        { key: 'make', label: 'Марка', type: 'text', required: true },
        { key: 'model', label: 'Модель', type: 'text', required: true },
        { key: 'year', label: 'Год выпуска', type: 'number', required: true },
        { key: 'vin', label: 'VIN', type: 'text', required: true, hint: '17 символов, есть в ПТС' },
      ],
    },
    property: {
      title: 'Объект недвижимости',
      fields: [
        { key: 'address', label: 'Адрес объекта', type: 'text', required: true },
        { key: 'cadastre_number', label: 'Кадастровый номер', type: 'text', required: true, hint: 'Например, 77:01:0001001:1234' },
        { key: 'area', label: 'Площадь, м²', type: 'number', required: false },
      ],
    },
    mortgage: {
      title: 'Ипотека',
      fields: [
        { key: 'object_price', label: 'Стоимость объекта', type: 'money', required: true },
        { key: 'down_payment', label: 'Первоначальный взнос', type: 'money', required: true },
      ],
    },
    target_business: {
      title: 'Приобретаемый бизнес',
      fields: [
        { key: 'inn', label: 'ИНН приобретаемого бизнеса', type: 'text', required: true },
        { key: 'name', label: 'Наименование', type: 'text', required: true },
        { key: 'deal_description', label: 'Краткое описание сделки', type: 'text', required: true },
        { key: 'turnover', label: 'Годовой оборот', type: 'money', required: true },
        { key: 'profit', label: 'Годовая прибыль', type: 'money', required: true },
      ],
    },
  };

  // Подписи значений select, которые на бэкенде хранятся кодами
  var OPTION_LABELS = {
    equipment: 'Оборудование',
    vehicle: 'Транспорт',
    real_estate: 'Недвижимость',
    other: 'Другое',
  };

  // collateral: none | optional | required; orgTypes пуст — и ИП, и ООО
  var PRODUCTS = {
    overdraft: { name: 'Овердрафт', term: [1, 2], collateral: 'none', groups: ['account', 'rko'], documents: ['account_statement'] },
    credit_line: { name: 'Возобновляемая кредитная линия (ВКЛ)', term: [1, 24], collateral: 'optional', groups: ['account', 'credit_line'], documents: ['account_statement'] },
    express: { name: 'Экспресс-кредит для бизнеса', term: [1, 18], collateral: 'none' },
    working_capital: { name: 'Кредит на пополнение оборотных средств', term: [3, 18], collateral: 'none', documents: ['financial_statements'] },
    marketplace_seller: { name: 'Кредит для селлеров маркетплейсов', term: [3, 18], collateral: 'none', groups: ['marketplace'], documents: ['marketplace_sales_report'] },
    ip_loan: { name: 'Кредит для ИП', term: [3, 18], collateral: 'none', orgTypes: ['IP'] },
    ooo_loan: { name: 'Кредит для ООО', term: [3, 18], collateral: 'none', orgTypes: ['OOO'] },
    loan_up_to_1m: { name: 'Кредит до 1 млн ₽', term: [3, 18], maxAmount: 1000000, collateral: 'none', documents: ['account_statement'] },
    equipment_loan: { name: 'Кредит на оборудование', term: [24, 84], collateral: 'required', groups: ['collateral', 'supply'], documents: ['title_documents', 'equipment_purchase_contract', 'equipment_specification'] },
    auto_loan: { name: 'Автокредит для бизнеса', term: [24, 84], collateral: 'required', groups: ['collateral', 'vehicle'], documents: ['title_documents', 'vehicle_registration'] },
    secured_property_equipment: { name: 'Кредит под залог недвижимости или оборудования', term: [3, 84], collateral: 'required', groups: ['collateral', 'property'], documents: ['title_documents', 'egrn_extract', 'technical_passport', 'valuation_report'] },
    business_purchase: { name: 'Кредит на покупку завода или готового бизнеса', term: [24, 84], collateral: 'required', groups: ['collateral', 'target_business'], documents: ['title_documents', 'business_plan', 'target_financial_statements', 'business_purchase_contract'] },
    mortgage_ip: { name: 'Ипотека для ИП', term: [24, 84], collateral: 'required', orgTypes: ['IP'], groups: ['collateral', 'property', 'mortgage'], documents: ['title_documents', 'preliminary_purchase_contract', 'egrn_extract', 'valuation_report'] },
    mortgage_business: { name: 'Ипотека для бизнеса (коммерческая недвижимость)', term: [24, 84], collateral: 'required', groups: ['collateral', 'property', 'mortgage'], documents: ['title_documents', 'preliminary_purchase_contract', 'egrn_extract', 'valuation_report'] },
  };

  // ---------- Соответствие id калькулятора и бэкенда ----------

  var FRONT_TO_BACK = {
    'working-capital': 'working_capital',
    'ooo-unsecured': 'ooo_loan',
    'ip-unsecured': 'ip_loan',
    seller: 'marketplace_seller',
    express: 'express',
    small: 'loan_up_to_1m',
    overdraft: 'overdraft',
    'credit-line': 'credit_line',
    equipment: 'equipment_loan',
    auto: 'auto_loan',
    'secured-property': 'secured_property_equipment',
    'mortgage-ooo': 'mortgage_business',
    'mortgage-ip': 'mortgage_ip',
    'ready-business': 'business_purchase',
  };

  var BACK_TO_FRONT = {};
  Object.keys(FRONT_TO_BACK).forEach(function (k) {
    BACK_TO_FRONT[FRONT_TO_BACK[k]] = k;
  });

  function backId(frontId) {
    return FRONT_TO_BACK[frontId] || null;
  }

  function frontId(id) {
    return BACK_TO_FRONT[id] || null;
  }

  // ---------- Цели кредита ----------
  // products — id калькулятора; подходящие отбираются по правилам доступности.
  // collateral: 'ask' — спросить про залог; 'with' — залог обязателен (покупаемое имущество);
  // 'without' — залог не нужен. collateralType — что подставить в анкету как тип залога.

  var GOALS = [
    {
      id: 'working', icon: 'coins', title: 'Пополнить оборот',
      text: 'Закупка товара, зарплаты, аренда, налоги',
      purpose: 'Пополнение оборотных средств',
      products: ['working-capital', 'ooo-unsecured', 'ip-unsecured', 'express', 'small', 'secured-property'],
      collateral: 'ask', amount: 1000000, term: 12,
    },
    {
      id: 'gap', icon: 'cycle', title: 'Закрыть кассовый разрыв',
      text: 'Деньги «на подхвате», когда клиенты платят с задержкой',
      purpose: 'Покрытие кассовых разрывов',
      products: ['overdraft', 'credit-line', 'express'],
      collateral: 'ask', amount: 500000, term: 2,
    },
    {
      id: 'marketplace', icon: 'bag', title: 'Товар для маркетплейса',
      text: 'Закупка под продажи на Wildberries, Ozon, Яндекс Маркете',
      purpose: 'Закупка товара для продаж на маркетплейсе',
      products: ['seller', 'working-capital', 'express', 'small'],
      collateral: 'without', amount: 1000000, term: 12,
    },
    {
      id: 'equipment', icon: 'gear', title: 'Купить оборудование',
      text: 'Станки, техника для производства и услуг',
      purpose: 'Покупка оборудования',
      products: ['equipment', 'secured-property'],
      collateral: 'with', collateralType: 'equipment', amount: 3000000, term: 36,
    },
    {
      id: 'auto', icon: 'car', title: 'Автомобиль или спецтехника',
      text: 'Легковой, грузовой транспорт, спецтехника',
      purpose: 'Покупка транспорта для бизнеса',
      products: ['auto'],
      collateral: 'with', collateralType: 'vehicle', amount: 3000000, term: 36,
    },
    {
      id: 'realty', icon: 'house', title: 'Коммерческая недвижимость',
      text: 'Офис, склад, магазин или производственное помещение',
      purpose: 'Покупка коммерческой недвижимости',
      products: ['mortgage-ooo', 'mortgage-ip', 'secured-property'],
      collateral: 'with', collateralType: 'real_estate', amount: 10000000, term: 60,
    },
    {
      id: 'business', icon: 'factory', title: 'Купить готовый бизнес',
      text: 'Действующее производство, компанию или долю',
      purpose: 'Покупка готового бизнеса',
      products: ['ready-business'],
      collateral: 'with', amount: 15000000, term: 60,
    },
  ];

  function goalById(id) {
    for (var i = 0; i < GOALS.length; i++) if (GOALS[i].id === id) return GOALS[i];
    return null;
  }

  // Цель, к которой относится продукт (для расчёта из калькулятора)
  function goalForProduct(frontProductId) {
    for (var i = 0; i < GOALS.length; i++) {
      if (GOALS[i].products.indexOf(frontProductId) !== -1) return GOALS[i];
    }
    return GOALS[0];
  }

  // ---------- Доступность и подбор ----------

  // q = { org: 'ooo'|'ip', collateral: 'with'|'without', age: 'gte9'|'lt9', amount, term }
  // Продукт должен подходить и по правилам калькулятора, и по каталогу бэкенда:
  // иначе сервер отклонит заявку при отправке (422).
  function isAvailable(frontProduct, q) {
    if (!Loans.isAvailable(frontProduct, q)) return false;
    var back = PRODUCTS[backId(frontProduct.id)];
    if (!back) return false;
    if (q.term < back.term[0] || q.term > back.term[1]) return false;
    if (back.maxAmount && q.amount > back.maxAmount) return false;
    if (back.orgTypes && back.orgTypes.indexOf(q.org === 'ip' ? 'IP' : 'OOO') === -1) return false;
    return true;
  }

  // Подходящие продукты цели по возрастанию ставки, с платежом и переплатой
  function recommend(goal, q) {
    return goal.products
      .map(Loans.productById)
      .filter(function (p) {
        return p && isAvailable(p, q);
      })
      .map(function (p) {
        var o = Loans.offer(p, q);
        return { product: p, backId: backId(p.id), payment: o.payment, overpayment: o.overpayment };
      })
      .sort(function (a, b) {
        return a.product.rate - b.product.rate;
      });
  }

  // Допустимые сроки и суммы по продуктам цели — чтобы подсказать, что поменять, если ничего не подошло
  function goalRanges(goal, q) {
    var termMin = Infinity, termMax = 0, amountMin = Infinity, amountMax = 0;
    goal.products.forEach(function (id) {
      var p = Loans.productById(id);
      var back = p && PRODUCTS[backId(id)];
      if (!back || p.org.indexOf(q.org) === -1) return;
      if (back.orgTypes && back.orgTypes.indexOf(q.org === 'ip' ? 'IP' : 'OOO') === -1) return;
      termMin = Math.min(termMin, Math.max(p.term[0], back.term[0]));
      termMax = Math.max(termMax, Math.min(p.term[1], back.term[1]));
      amountMin = Math.min(amountMin, p.amount[0]);
      amountMax = Math.max(amountMax, Math.min(Loans.maxAmountFor(p, q.age), back.maxAmount || Infinity));
    });
    if (!isFinite(termMin)) return null;
    return { term: [termMin, termMax], amount: [amountMin, amountMax] };
  }

  // ---------- Поля и документы ----------

  function groupsFor(id) {
    var p = PRODUCTS[id];
    var names = BASE_GROUPS.concat((p && p.groups) || []);
    return names.map(function (name) {
      return { name: name, title: FIELD_GROUPS[name].title, fields: FIELD_GROUPS[name].fields };
    });
  }

  function documentsFor(id) {
    var p = PRODUCTS[id];
    var required = [];
    BASE_DOCUMENTS.concat((p && p.documents) || []).forEach(function (d) {
      if (required.indexOf(d) === -1) required.push(d);
    });
    return {
      required: required.map(function (d) { return { kind: d, title: DOCUMENTS[d] }; }),
      optional: BASE_OPTIONAL_DOCUMENTS.map(function (d) { return { kind: d, title: DOCUMENTS[d] }; }),
    };
  }

  // ---------- Составные поля ----------
  // Клиент вводит номер и дату договора поставки отдельно, а бэкенд ждёт одну строку contract_details.
  // В форме храним и части (чтобы черновик открывался с ними), и склеенную строку.

  var CONTRACT_RE = /^№\s*(.+?)\s+от\s+(\d{2})\.(\d{2})\.(\d{4})$/;

  function composeForm(form) {
    var supply = form.supply;
    if (!supply) return form;
    var out = Object.assign({}, form, { supply: Object.assign({}, supply) });
    if (supply.contract_number && supply.contract_date) {
      out.supply.contract_details = '№ ' + supply.contract_number + ' от ' + DatePicker.format(supply.contract_date);
    } else {
      delete out.supply.contract_details;
    }
    return out;
  }

  // Черновик, сохранённый до разделения полей: разбираем строку обратно на номер и дату
  function decomposeForm(form) {
    var supply = form.supply;
    if (!supply || !supply.contract_details || supply.contract_number) return form;
    var m = CONTRACT_RE.exec(supply.contract_details);
    supply.contract_number = m ? m[1] : supply.contract_details;
    if (m) supply.contract_date = m[4] + '-' + m[3] + '-' + m[2];
    return form;
  }

  // Ошибка сервера по склеенному полю подсвечивает обе его части
  var FIELD_ALIASES = { 'supply.contract_details': ['supply.contract_number', 'supply.contract_date'] };

  function fieldPaths(path) {
    return FIELD_ALIASES[path] || [path];
  }

  // «applicant.position» → «Заявитель: Должность в компании»; для полей вне формы — понятные названия
  var TOP_FIELDS = { product_id: 'Продукт', amount: 'Сумма', term_months: 'Срок', purpose: 'Цель кредита' };

  function fieldLabel(path) {
    if (TOP_FIELDS[path]) return TOP_FIELDS[path];
    if (path === 'supply.contract_details') return 'Поставка оборудования: номер и дата договора';
    var parts = path.split('.');
    var group = FIELD_GROUPS[parts[0]];
    if (!group) return path;
    for (var i = 0; i < group.fields.length; i++) {
      if (group.fields[i].key === parts[1]) return group.title + ': ' + group.fields[i].label;
    }
    return group.title;
  }

  // ---------- Статусы заявки ----------
  // tone: neutral | progress | success | warning | danger. step — этап на шкале (см. STAGES).

  var STAGES = ['Черновик', 'Подана', 'Проверка', 'Решение', 'Подписание', 'Выдача'];

  var STATUS = {
    DRAFT: { label: 'Черновик', tone: 'neutral', step: 0, text: 'Заявка сохранена, но ещё не отправлена в банк.' },
    SUBMITTED: { label: 'Подана', tone: 'progress', step: 1, text: 'Заявка отправлена. Банк скоро начнёт проверку.' },
    DATA_CHECK: { label: 'Проверка данных', tone: 'progress', step: 2, text: 'Проверяем сведения о компании в ЕГРЮЛ, ФНС и БКИ.' },
    INFO_REQUESTED: { label: 'Нужны данные', tone: 'warning', step: 2, text: 'Банку нужны дополнительные сведения или документы.' },
    SCORING: { label: 'Оценка', tone: 'progress', step: 2, text: 'Оцениваем финансовое положение и кредитную историю.' },
    MANUAL_REVIEW: { label: 'У андеррайтера', tone: 'progress', step: 3, text: 'Заявку проверяет специалист банка.' },
    APPROVED: { label: 'Одобрена', tone: 'success', step: 3, text: 'Кредит одобрен. Осталось подписать договор.' },
    COUNTER_OFFER: { label: 'Встречное предложение', tone: 'warning', step: 3, text: 'Банк готов выдать кредит на других условиях.' },
    REJECTED: { label: 'Отказ', tone: 'danger', step: 3, text: 'К сожалению, банк не может выдать кредит на этих условиях.' },
    SIGNING: { label: 'Подписание', tone: 'progress', step: 4, text: 'Договор готов к подписанию.' },
    SIGNED: { label: 'Договор подписан', tone: 'success', step: 4, text: 'Договор подписан, деньги скоро поступят на счёт.' },
    DISBURSED: { label: 'Деньги выданы', tone: 'success', step: 5, text: 'Деньги перечислены на расчётный счёт.' },
    ACTIVE: { label: 'Кредит действует', tone: 'success', step: 5, text: 'Кредит выдан и обслуживается.' },
    OVERDUE: { label: 'Просрочка', tone: 'danger', step: 5, text: 'Есть просроченный платёж.' },
    CLOSED: { label: 'Закрыт', tone: 'neutral', step: 5, text: 'Кредит полностью погашен.' },
  };

  function status(code) {
    return STATUS[code] || { label: code, tone: 'neutral', step: 0, text: '' };
  }

  window.Catalog = {
    GOALS: GOALS,
    PRODUCTS: PRODUCTS,
    OPTION_LABELS: OPTION_LABELS,
    STAGES: STAGES,
    backId: backId,
    frontId: frontId,
    goalById: goalById,
    goalForProduct: goalForProduct,
    isAvailable: isAvailable,
    recommend: recommend,
    goalRanges: goalRanges,
    groupsFor: groupsFor,
    documentsFor: documentsFor,
    fieldLabel: fieldLabel,
    fieldPaths: fieldPaths,
    composeForm: composeForm,
    decomposeForm: decomposeForm,
    status: status,
  };
})();
