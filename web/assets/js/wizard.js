/*
 * wizard.js — мастер нового кредита в личном кабинете.
 *
 * Клиенту не нужно разбираться в 14 продуктах: он отвечает, на что нужны деньги, выбирает сумму и срок,
 * а продукт подбирается сам. Шаги:
 *   1. Цель        — «на что нужны деньги» (Catalog.GOALS)
 *   2. Сумма и срок — плюс вопрос про залог, если он зависит от клиента
 *   3. Предложение — лучший продукт и 1–2 альтернативы, влияние на долговую нагрузку
 *   4. Анкета      — только поля выбранного продукта; известное о компании подставлено
 *   5. Документы   — чек-лист и согласие на запрос в БКИ
 *   6. Проверка    — всё вместе и отправка
 *
 * Бэкенд (настоящий):
 *   POST  /applications            — черновик создаётся при переходе с шага 3 на шаг 4
 *   PATCH /applications/{id}       — сохранение на каждом шаге (нужна актуальная version)
 *   POST  /applications/{id}/submit — отправка; 422 { missing_fields } — что не заполнено
 *   GET   /applications/{id}       — продолжение черновика (#draft=<id>)
 * Все изменяющие запросы идут с заголовком Idempotency-Key.
 *
 * Файлы документов пока никуда не отправляются: метода загрузки в main ещё нет.
 *
 * Снаружи: Wizard.open({ context, applicationId, calcDraft }); Wizard.onExit(changed) задаёт cabinet.js.
 */
(function () {
  'use strict';

  var $ = function (id) {
    return document.getElementById(id);
  };
  var icon = UI.icon;
  var esc = UI.esc;
  var fmt = UI.fmt;

  var STEPS = ['Цель', 'Сумма и срок', 'Предложение', 'Анкета', 'Документы', 'Проверка'];
  var STEP = { GOAL: 0, PARAMS: 1, OFFER: 2, FORM: 3, DOCS: 4, REVIEW: 5 };

  var AMOUNT_STOPS = Loans.AMOUNT_STOPS;
  var TERM_STOPS = Loans.TERM_STOPS;

  var s = null; // состояние мастера, см. reset()

  function reset(context) {
    var company = context.company;
    var regMonths = company ? UI.monthsSince(company.registration_date) : null;
    s = {
      ctx: context,
      step: STEP.GOAL,
      done: false,
      busy: false,
      changed: false, // создавали или меняли заявку — кабинету нужно обновить список
      org: company && company.org_type === 'IP' ? 'ip' : 'ooo',
      age: regMonths == null || regMonths >= 9 ? 'gte9' : 'lt9',
      goalId: null,
      collateral: 'without',
      amount: 1000000,
      term: 12,
      productId: null, // id калькулятора (loans.js); на сервер уходит Catalog.backId(productId)
      fromCalc: null, // расчёт из калькулятора, если мастер открыт после него
      app: null, // { id, version } — черновик на сервере
      purpose: '',
      form: {},
      files: {}, // вид документа → имя загруженного на сервер файла
      consents: { bki: false, pd: false },
      errors: {}, // ошибки полей анкеты: 'группа.поле' → текст
      dateRaw: {}, // что введено в поля дат как текст: нужно, чтобы отличить «пусто» от «такой даты нет»
      savedAt: null,
    };
  }

  function query() {
    return { org: s.org, collateral: s.collateral, age: s.age, amount: s.amount, term: s.term };
  }

  function goal() {
    return Catalog.goalById(s.goalId);
  }

  // ---------- Ошибки и «сохранено» ----------

  function showError(text) {
    $('wizard-error-message').textContent = text;
    $('wizard-error').classList.add('visible');
    $('wizard-error').scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  }

  function hideError() {
    $('wizard-error').classList.remove('visible');
  }

  function serverError(res) {
    if (res.status === 0) return 'Не удалось связаться с сервером. Проверьте подключение и попробуйте ещё раз.';
    if (res.status === 409) return 'Заявка уже отправлена и больше не редактируется.';
    if (res.status === 422) return 'Этот продукт недоступен вашей компании на выбранных условиях. Измените сумму или срок.';
    if (res.status === 400) return 'Сервер не принял данные заявки. Проверьте введённые значения.';
    return 'Что-то пошло не так. Попробуйте ещё раз через минуту.';
  }

  function renderSaved() {
    var el = $('wizard-saved');
    if (!s.app) {
      el.textContent = '';
      return;
    }
    var t = s.savedAt ? s.savedAt.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' }) : '';
    el.textContent = 'Черновик сохранён' + (t ? ' в ' + t : '');
  }

  // ---------- Запросы к заявкам ----------

  // Ключ идемпотентности повторяется только для того же действия с тем же телом:
  // так повтор после сетевого сбоя не создаст вторую заявку.
  var pending = {};

  async function mutate(path, method, body) {
    var sig = method + ' ' + path + ' ' + JSON.stringify(body || null);
    var key = pending[sig] || (pending[sig] = api.newKey());
    var res = await api(path, { method: method, json: body, idempotencyKey: key });
    if (res.status !== 0) delete pending[sig];
    return res;
  }

  // В форму уходят только группы текущего продукта: если клиент вернулся и сменил цель,
  // поля прежнего продукта на сервер не попадут. Согласия хранятся там же, в form.consents.
  function draftBody() {
    var backId = Catalog.backId(s.productId);
    var form = {};
    if (backId) {
      Catalog.groupsFor(backId).forEach(function (g) {
        if (s.form[g.name]) form[g.name] = s.form[g.name];
      });
    }
    form = Catalog.composeForm(form);
    form.consents = { bki_request: s.consents.bki, personal_data: s.consents.pd };
    return {
      product_id: backId || '',
      amount: s.amount,
      term_months: s.term,
      purpose: s.purpose,
      form: form,
    };
  }

  function remember(app) {
    s.app = { id: app.id, version: app.version };
    s.savedAt = new Date();
    s.changed = true;
    renderSaved();
  }

  // Создать черновик или сохранить изменения. При конфликте версий перечитываем заявку и пробуем ещё раз.
  async function save() {
    var res;
    if (!s.app) {
      res = await mutate('/applications', 'POST', draftBody());
    } else {
      res = await mutate('/applications/' + encodeURIComponent(s.app.id), 'PATCH', Object.assign({ version: s.app.version }, draftBody()));
      if (res.status === 409 && /changed/.test((res.data && res.data.error) || '')) {
        var fresh = await api('/applications/' + encodeURIComponent(s.app.id));
        if (fresh.ok) {
          s.app.version = fresh.data.version;
          res = await mutate('/applications/' + encodeURIComponent(s.app.id), 'PATCH', Object.assign({ version: s.app.version }, draftBody()));
        }
      }
    }
    if (res.ok) remember(res.data);
    return res;
  }

  // ---------- Отрисовка ----------

  function renderStepper() {
    $('wizard-stepper').innerHTML = STEPS.map(function (name, i) {
      var cls = s.done || i < s.step ? 'done' : i === s.step ? 'current' : '';
      var clickable = !s.done && i < s.step;
      return (
        '<li class="' + cls + '">' +
        (clickable ? '<button type="button" data-step="' + i + '">' : '<span>') +
        '<span class="step-num">' + (cls === 'done' ? icon('check', 14) : i + 1) + '</span>' +
        '<span class="step-name">' + name + '</span>' +
        (clickable ? '</button>' : '</span>') +
        '</li>'
      );
    }).join('');
  }

  function render() {
    renderStepper();
    var body = $('wizard-body');
    var renderers = [renderGoal, renderParams, renderOffer, renderForm, renderDocs, renderReview];
    body.innerHTML = s.done ? renderDone() : renderers[s.step]();
    if (!s.done) bindStep[s.step]();

    var next = $('wizard-next');
    var prev = $('wizard-prev');
    $('wizard-foot').classList.toggle('hidden', s.done);
    prev.classList.toggle('invisible', s.step === STEP.GOAL);
    prev.disabled = s.busy;
    next.disabled = s.busy || !canContinue();
    var labels = { 2: s.app ? 'Далее' : 'Оформить заявку', 5: 'Отправить заявку' };
    next.innerHTML = s.busy ? '<div class="spinner" aria-hidden="true"></div><span>Сохраняем…</span>' : '<span>' + (labels[s.step] || 'Далее') + '</span>';
    renderSaved();
  }

  function canContinue() {
    if (s.step === STEP.GOAL) return !!s.goalId;
    if (s.step === STEP.PARAMS) return Catalog.recommend(goal(), query()).length > 0;
    if (s.step === STEP.OFFER) return !!s.productId;
    if (s.step === STEP.DOCS) return s.consents.bki && s.consents.pd;
    return true;
  }

  // --- Шаг 1. Цель ---

  function renderGoal() {
    return (
      '<h2 class="step-title">На что нужны деньги?</h2>' +
      '<p class="step-lead">Выберите задачу — подходящий кредит мы подберём сами. ' +
      'Тип бизнеса и срок работы уже знаем: ' + (s.org === 'ip' ? 'ИП' : 'ООО') + ', ' +
      (s.age === 'gte9' ? 'больше 9 месяцев' : 'меньше 9 месяцев') + '.</p>' +
      '<div class="goals" role="radiogroup" aria-label="Цель кредита">' +
      Catalog.GOALS.map(function (g) {
        var available = g.products.some(function (id) {
          var p = Loans.productById(id);
          return p && p.org.indexOf(s.org) !== -1;
        });
        if (!available) return '';
        var on = g.id === s.goalId;
        return (
          '<button type="button" class="goal' + (on ? ' selected' : '') + '" role="radio" aria-checked="' + on + '" data-goal="' + g.id + '">' +
          '<span class="goal-icon">' + icon(g.icon, 24) + '</span>' +
          '<span class="goal-title">' + esc(g.title) + '</span>' +
          '<span class="goal-text">' + esc(g.text) + '</span>' +
          '</button>'
        );
      }).join('') +
      '</div>'
    );
  }

  function selectGoal(id) {
    var g = Catalog.goalById(id);
    if (s.goalId !== id) {
      s.goalId = id;
      s.amount = g.amount;
      s.term = g.term;
      s.collateral = g.collateral === 'with' ? 'with' : 'without';
      s.productId = null;
      s.purpose = g.purpose;
    }
  }

  // --- Шаг 2. Сумма и срок ---

  function nearest(stops, value) {
    var best = 0;
    for (var i = 1; i < stops.length; i++) {
      if (Math.abs(stops[i] - value) < Math.abs(stops[best] - value)) best = i;
    }
    return best;
  }

  function renderParams() {
    var g = goal();
    var collateralQuestion =
      g.collateral === 'ask'
        ? '<div class="param">' +
          '<div class="param-label">Можете предложить залог?</div>' +
          '<div class="segmented" role="radiogroup">' +
          '<button type="button" role="radio" data-collateral="without" aria-checked="' + (s.collateral === 'without') + '">Нет, без залога</button>' +
          '<button type="button" role="radio" data-collateral="with" aria-checked="' + (s.collateral === 'with') + '">Да, есть имущество</button>' +
          '</div>' +
          '<div class="param-hint">Недвижимость, оборудование или транспорт. С залогом ставка ниже, а сумма больше.</div>' +
          '</div>'
        : '';

    return (
      '<h2 class="step-title">' + esc(g.title) + ': сколько и на какой срок?</h2>' +
      '<p class="step-lead">Ориентировочно — точные условия будут в решении банка.</p>' +
      '<div class="params">' +
      '<div class="param">' +
      '<label class="param-label" for="p-amount">Сумма</label>' +
      '<div class="param-value"><input type="text" inputmode="numeric" id="p-amount-text" class="param-input" value="' + Loans.fmt.number(s.amount) + '" aria-label="Сумма, рублей" /><span>₽</span></div>' +
      '<input type="range" id="p-amount" class="range" min="0" max="' + (AMOUNT_STOPS.length - 1) + '" value="' + nearest(AMOUNT_STOPS, s.amount) + '" />' +
      '<div class="range-scale"><span>' + fmt.short(AMOUNT_STOPS[0]) + '</span><span>' + fmt.short(AMOUNT_STOPS[AMOUNT_STOPS.length - 1]) + '</span></div>' +
      '</div>' +
      '<div class="param">' +
      '<label class="param-label" for="p-term">Срок</label>' +
      '<div class="param-value"><span class="param-static" id="p-term-text">' + fmt.term(s.term) + '</span></div>' +
      '<input type="range" id="p-term" class="range" min="0" max="' + (TERM_STOPS.length - 1) + '" value="' + nearest(TERM_STOPS, s.term) + '" />' +
      '<div class="range-scale"><span>1 мес.</span><span>10 лет</span></div>' +
      '</div>' +
      collateralQuestion +
      '</div>' +
      '<div class="params-preview" id="params-preview" aria-live="polite">' + paramsPreview() + '</div>'
    );
  }

  function paramsPreview() {
    var list = Catalog.recommend(goal(), query());
    if (list.length) {
      return (
        icon('check', 18) +
        '<span>Нашли ' + list.length + ' ' + UI.plural(list.length, 'вариант', 'варианта', 'вариантов') +
        ' · платёж от <strong>' + fmt.rub(list[0].payment) + '</strong> в месяц</span>'
      );
    }
    var r = Catalog.goalRanges(goal(), query());
    var tip = r
      ? 'Для этой задачи подойдёт срок ' + Loans.fmt.termRange(r.term[0], r.term[1]) + ' и сумма ' + Loans.fmt.amountRange(r.amount[0], r.amount[1]) + '.'
      : 'Для вашего типа бизнеса эта задача пока не поддерживается.';
    if (s.collateral === 'without' && goal().collateral === 'ask') tip += ' С залогом вариантов больше.';
    return icon('info', 18) + '<span>На этих условиях вариантов нет. ' + esc(tip) + '</span>';
  }

  function updatePreview() {
    $('params-preview').innerHTML = paramsPreview();
    $('wizard-next').disabled = !canContinue();
  }

  // --- Шаг 3. Предложение ---

  function impact(choice) {
    var b = s.ctx.bootstrap;
    var monthly = s.ctx.monthlyPayments + choice.payment;
    var html =
      '<div class="impact">' +
      '<div class="impact-title">Как изменится нагрузка</div>' +
      '<div class="impact-row"><span>Платежи в месяц</span><span>' + fmt.rub(s.ctx.monthlyPayments) + ' → <strong>' + fmt.rub(monthly) + '</strong></span></div>';
    if (b && b.total_debt != null && b.equity > 0) {
      var after = (b.total_debt + s.amount) / b.equity;
      var level = UI.debtLevel(after);
      html +=
        '<div class="impact-row"><span>Долг / капитал</span><span>' + fmt.ratio(b.debt_to_equity) + ' → <strong>' + fmt.ratio(after) + '</strong> ' +
        '<span class="level level-' + level.tone + '">' + level.label + '</span></span></div>';
      if (after > 2) {
        html += '<div class="impact-warn">' + icon('alert', 16) + '<span>Нагрузка станет выше 2 — это снижает шансы на одобрение. Попробуйте уменьшить сумму' +
          (s.collateral === 'without' ? ' или предложить залог' : '') + '.</span></div>';
      }
    }
    return html + '</div>';
  }

  function offerCard(o, best) {
    var p = o.product;
    var on = p.id === s.productId;
    return (
      '<button type="button" class="offer-card' + (on ? ' selected' : '') + (best ? ' best' : '') + '" data-product="' + p.id + '" role="radio" aria-checked="' + on + '">' +
      (best ? '<span class="offer-card-badge">Лучшее предложение</span>' : '') +
      '<span class="offer-card-title">' + esc(p.title) + '</span>' +
      '<span class="offer-card-desc">' + esc(p.desc) + '</span>' +
      '<span class="offer-card-grid">' +
      '<span><span class="oc-label">Платёж в месяц</span><span class="oc-value">' + fmt.rub(o.payment) + '</span></span>' +
      '<span><span class="oc-label">Ставка</span><span class="oc-value">' + fmt.rate(p.rate) + '</span></span>' +
      '<span><span class="oc-label">Переплата</span><span class="oc-value">' + fmt.rub(o.overpayment) + '</span></span>' +
      '</span>' +
      '</button>'
    );
  }

  function renderOffer() {
    var list = Catalog.recommend(goal(), query());
    if (!s.productId || !list.some(function (o) { return o.product.id === s.productId; })) {
      s.productId = list.length ? list[0].product.id : null;
    }
    var chosen = list.filter(function (o) { return o.product.id === s.productId; })[0];

    var calcNote = '';
    if (s.fromCalc) {
      calcNote = s.fromCalc.lost
        ? '<div class="notice notice-warning notice-compact">' + icon('info', 18) + '<div>Кредит, выбранный в калькуляторе, на этих условиях оформить нельзя. Мы подобрали похожие варианты.</div></div>'
        : '<div class="notice notice-info notice-compact">' + icon('info', 18) + '<div>Параметры перенесены из калькулятора. Сумму и срок можно поменять на предыдущем шаге.</div></div>';
    }

    return (
      '<h2 class="step-title">Мы подобрали для вас</h2>' +
      '<p class="step-lead">' + fmt.rub(s.amount) + ' на ' + fmt.term(s.term) + (s.collateral === 'with' ? ', с залогом' : ', без залога') + '.</p>' +
      calcNote +
      '<div class="offers" role="radiogroup" aria-label="Кредитные продукты">' +
      list.slice(0, 3).map(function (o, i) { return offerCard(o, i === 0); }).join('') +
      '</div>' +
      (chosen ? impact(chosen) : '') +
      '<p class="fine">Ставки предварительные. Окончательные условия банк сообщит в решении по заявке.</p>'
    );
  }

  // --- Шаг 4. Анкета ---

  function getField(group, key) {
    return s.form[group] ? s.form[group][key] : undefined;
  }

  function setField(group, key, value) {
    if (!s.form[group]) s.form[group] = {};
    if (value === '' || value == null) delete s.form[group][key];
    else s.form[group][key] = value;
  }

  // Подставляем то, что уже знаем о компании и о выбранном кредите. Заполненное клиентом не трогаем.
  function prefill() {
    var c = s.ctx.company || {};
    var u = s.ctx.user || {};
    var g = goal();
    var auto = {
      'applicant.position': u.position,
      'applicant.has_collateral': s.collateral === 'with',
      'account.account_number': c.account_number,
      'rko.has_settlement_account': c.account_number ? true : undefined,
      'credit_line.desired_limit': s.amount,
      'collateral.type': g && g.collateralType,
    };
    s.prefilled = {};
    Object.keys(auto).forEach(function (path) {
      var parts = path.split('.');
      if (auto[path] === undefined || auto[path] === '') return;
      if (getField(parts[0], parts[1]) === undefined) setField(parts[0], parts[1], auto[path]);
      if (getField(parts[0], parts[1]) === auto[path]) s.prefilled[path] = true;
    });
    // Наличие залога определяется выбранным продуктом и ответом на шаге 2
    setField('applicant', 'has_collateral', s.collateral === 'with');
  }

  function fieldHtml(group, f) {
    var path = group + '.' + f.key;
    var id = 'f-' + group + '-' + f.key;
    var value = getField(group, f.key);
    var err = s.errors[path];
    var tag = s.prefilled && s.prefilled[path] ? '<span class="prefill-tag">из профиля</span>' : '';
    var label = '<label class="field-label" for="' + id + '">' + esc(f.label) + (f.required ? '' : ' <span class="optional">необязательно</span>') + tag + '</label>';
    var control;

    if (f.type === 'bool') {
      // Наличие залога не спрашиваем повторно — оно уже выбрано
      if (path === 'applicant.has_collateral') {
        return '<div class="field field-readonly"><div class="field-label">' + esc(f.label) + '</div><div class="readonly-value">' + (value ? 'Да' : 'Нет') + '</div></div>';
      }
      control =
        '<div class="segmented" role="radiogroup" id="' + id + '">' +
        '<button type="button" role="radio" data-field="' + path + '" data-value="true" aria-checked="' + (value === true) + '">Да</button>' +
        '<button type="button" role="radio" data-field="' + path + '" data-value="false" aria-checked="' + (value === false) + '">Нет</button>' +
        '</div>';
      label = '<div class="field-label" id="' + id + '-label">' + esc(f.label) + tag + '</div>';
    } else if (f.type === 'select') {
      control =
        '<select class="text-input" id="' + id + '" data-field="' + path + '">' +
        '<option value="">Выберите</option>' +
        f.options.map(function (o) {
          return '<option value="' + esc(o) + '"' + (value === o ? ' selected' : '') + '>' + esc(Catalog.OPTION_LABELS[o] || o) + '</option>';
        }).join('') +
        '</select>';
    } else if (f.type === 'date') {
      control = DatePicker.html({ id: id, value: value, max: f.max, attrs: 'data-field="' + path + '" data-type="date"' });
    } else if (f.type === 'money') {
      control =
        '<div class="money-input"><input type="text" inputmode="numeric" id="' + id + '" data-field="' + path + '" data-type="money" value="' +
        (value != null ? Loans.fmt.number(value) : '') + '" autocomplete="off" /><span>₽</span></div>';
    } else {
      control =
        '<input type="text" class="text-input" id="' + id + '" data-field="' + path + '" data-type="' + f.type + '"' +
        (f.type === 'number' ? ' inputmode="decimal"' : '') + ' value="' + esc(value != null ? value : '') + '" autocomplete="off" />';
    }

    return (
      '<div class="field' + (err ? ' has-error' : '') + '">' + label + control +
      (err ? '<div class="field-error">' + esc(err) + '</div>' : f.hint ? '<div class="field-hint">' + esc(f.hint) + '</div>' : '') +
      '</div>'
    );
  }

  function renderForm() {
    var backId = Catalog.backId(s.productId);
    var groups = Catalog.groupsFor(backId);
    var purposeErr = s.errors.purpose;
    return (
      '<h2 class="step-title">Анкета</h2>' +
      '<p class="step-lead">' + esc(Catalog.PRODUCTS[backId].name) + ' · ' + fmt.rub(s.amount) + ' на ' + fmt.term(s.term) +
      '. Сведения о компании возьмём из ЕГРЮЛ — вводить их не нужно.</p>' +
      '<fieldset class="form-group"><legend>Цель кредита</legend>' +
      '<div class="field' + (purposeErr ? ' has-error' : '') + '">' +
      '<label class="field-label" for="f-purpose">Коротко опишите, на что пойдут деньги</label>' +
      '<textarea class="text-input" id="f-purpose" rows="2" data-field="purpose">' + esc(s.purpose) + '</textarea>' +
      (purposeErr ? '<div class="field-error">' + esc(purposeErr) + '</div>' : '') +
      '</div></fieldset>' +
      groups.map(function (g) {
        return (
          '<fieldset class="form-group"><legend>' + esc(g.title) + '</legend>' +
          '<div class="fields">' + g.fields.map(function (f) { return fieldHtml(g.name, f); }).join('') + '</div>' +
          '</fieldset>'
        );
      }).join('')
    );
  }

  function validateForm() {
    var errors = {};
    if (!String(s.purpose || '').trim()) errors.purpose = 'Укажите цель кредита';
    Catalog.groupsFor(Catalog.backId(s.productId)).forEach(function (g) {
      g.fields.forEach(function (f) {
        var v = getField(g.name, f.key);
        var path = g.name + '.' + f.key;
        if (f.type === 'date') {
          var raw = s.dateRaw[path];
          if (raw && !v) {
            errors[path] = raw.length < 10 ? 'Введите дату полностью: ДД.ММ.ГГГГ' : 'Такой даты нет — проверьте день и месяц';
            return;
          }
          if (v && f.max === 'today' && UI.daysUntil(v) > 0) {
            errors[path] = 'Дата не может быть позже сегодняшней';
            return;
          }
        }
        if (f.required && (v === undefined || v === null || String(v).trim() === '')) {
          errors[path] = f.type === 'bool' || f.type === 'select' ? 'Выберите вариант' : 'Заполните поле';
          return;
        }
        if (v === undefined) return;
        if (path === 'account.account_number' && !/^\d{20}$/.test(String(v))) errors[path] = 'Номер счёта — 20 цифр';
        if (path === 'vehicle.vin' && !/^[A-HJ-NPR-Z0-9]{17}$/i.test(String(v))) errors[path] = 'VIN — 17 латинских букв и цифр';
        if (path === 'vehicle.year' && (v < 1980 || v > new Date().getFullYear() + 1)) errors[path] = 'Проверьте год выпуска';
        if (path === 'marketplace.revenue_share' && (v < 0 || v > 100)) errors[path] = 'Доля — от 0 до 100%';
        if (path === 'target_business.inn' && !/^(\d{10}|\d{12})$/.test(String(v))) errors[path] = 'ИНН — 10 или 12 цифр';
      });
    });
    s.errors = errors;
    return Object.keys(errors).length === 0;
  }

  // --- Шаг 5. Документы ---

  function renderDocs() {
    var docs = Catalog.documentsFor(Catalog.backId(s.productId));
    function docRow(d, optional) {
      var file = s.files[d.kind];
      return (
        '<li class="doc' + (file ? ' attached' : '') + '">' +
        '<span class="doc-icon">' + icon(file ? 'check' : 'file', 18) + '</span>' +
        '<span class="doc-text"><span class="doc-title">' + esc(d.title) + '</span>' +
        '<span class="doc-sub">' + (file ? esc(file) : optional ? 'необязательно' : 'обязательно · PDF, JPG или PNG до 10 МБ') + '</span></span>' +
        '<label class="btn btn-secondary btn-inline doc-btn">' + (file ? 'Заменить' : 'Прикрепить') +
        '<input type="file" class="visually-hidden" data-doc="' + d.kind + '" accept=".pdf,.jpg,.jpeg,.png" /></label>' +
        '</li>'
      );
    }
    return (
      '<h2 class="step-title">Документы и согласия</h2>' +
      '<p class="step-lead">Выписку из ЕГРЮЛ и кредитную историю банк запросит сам. Остальное можно приложить сейчас или дослать по запросу банка.</p>' +
      '<ul class="docs">' +
      docs.required.map(function (d) { return docRow(d, false); }).join('') +
      docs.optional.map(function (d) { return docRow(d, true); }).join('') +
      '</ul>' +
      '<p class="fine doc-error" id="doc-error" hidden></p>' +
      '<div class="consents">' +
      '<label class="check"><input type="checkbox" id="c-bki"' + (s.consents.bki ? ' checked' : '') + ' />' +
      '<span>Даю согласие на запрос моей кредитной истории и кредитной истории компании в бюро кредитных историй</span></label>' +
      '<label class="check"><input type="checkbox" id="c-pd"' + (s.consents.pd ? ' checked' : '') + ' />' +
      '<span>Даю согласие на обработку персональных данных для рассмотрения заявки</span></label>' +
      '</div>'
    );
  }

  // --- Шаг 6. Проверка ---

  function reviewRow(label, value) {
    return '<div class="kv"><dt>' + esc(label) + '</dt><dd>' + value + '</dd></div>';
  }

  function displayValue(f, v) {
    if (v === undefined || v === null || v === '') return '<span class="muted">не указано</span>';
    if (f.type === 'bool') return v ? 'Да' : 'Нет';
    if (f.type === 'money') return fmt.rub(v);
    if (f.type === 'select') return esc(Catalog.OPTION_LABELS[v] || v);
    if (f.type === 'date') return esc(DatePicker.format(v));
    return esc(v);
  }

  function renderReview() {
    var backId = Catalog.backId(s.productId);
    var p = Loans.productById(s.productId);
    var o = Loans.offer(p, query());
    var docs = Catalog.documentsFor(backId).required;
    var missingDocs = docs.filter(function (d) { return !s.files[d.kind]; });
    var c = s.ctx.company || {};

    return (
      '<h2 class="step-title">Проверьте заявку</h2>' +
      '<p class="step-lead">После отправки изменить заявку будет нельзя. Статус рассмотрения появится в кабинете.</p>' +

      '<section class="review"><div class="review-head"><h3>Кредит</h3><button type="button" class="link" data-step="' + STEP.PARAMS + '">Изменить</button></div>' +
      '<dl class="kv-list kv-grid">' +
      reviewRow('Продукт', esc(Catalog.PRODUCTS[backId].name)) +
      reviewRow('Сумма', fmt.rub(s.amount)) +
      reviewRow('Срок', fmt.term(s.term)) +
      reviewRow('Платёж в месяц', '≈ ' + fmt.rub(o.payment) + ' <span class="muted">при ставке ' + fmt.rate(p.rate) + '</span>') +
      '</dl></section>' +

      '<section class="review"><div class="review-head"><h3>Компания</h3><span class="source-badge">из ЕГРЮЛ</span></div>' +
      '<dl class="kv-list kv-grid">' +
      reviewRow('Название', esc(c.name || '—')) +
      reviewRow('ИНН', esc(c.inn || '—')) +
      '</dl></section>' +

      '<section class="review"><div class="review-head"><h3>Анкета</h3><button type="button" class="link" data-step="' + STEP.FORM + '">Изменить</button></div>' +
      '<dl class="kv-list kv-grid">' +
      reviewRow('Цель кредита', esc(s.purpose)) +
      Catalog.groupsFor(backId).map(function (g) {
        return g.fields.map(function (f) { return reviewRow(f.label, displayValue(f, getField(g.name, f.key))); }).join('');
      }).join('') +
      '</dl></section>' +

      '<section class="review"><div class="review-head"><h3>Документы</h3><button type="button" class="link" data-step="' + STEP.DOCS + '">Изменить</button></div>' +
      (missingDocs.length
        ? '<div class="notice notice-warning notice-compact">' + icon('info', 18) + '<div>Не приложено: ' +
          esc(missingDocs.map(function (d) { return d.title; }).join('; ')) + '. Заявку можно отправить — банк запросит документы, если они понадобятся.</div></div>'
        : '<p class="ok-line">' + icon('check', 18) + 'Все документы приложены</p>') +
      '<p class="ok-line">' + icon('check', 18) + 'Согласие на запрос в БКИ и обработку персональных данных</p>' +
      '</section>'
    );
  }

  // --- Готово ---

  function renderDone() {
    return (
      '<div class="wizard-done">' +
      '<div class="wizard-done-icon">' + icon('check', 34) + '</div>' +
      '<h2 class="step-title">Заявка отправлена</h2>' +
      '<p class="step-lead">Банк проверит сведения о компании и кредитную историю и примет решение. ' +
      'Статус заявки виден в кабинете — если понадобятся документы или будет встречное предложение, мы покажем это там же.</p>' +
      '<button type="button" class="btn btn-primary btn-inline" id="wizard-finish">Вернуться в кабинет</button>' +
      '</div>'
    );
  }

  // ---------- Обработчики шагов ----------

  var bindStep = [
    function bindGoal() {
      $('wizard-body').querySelectorAll('[data-goal]').forEach(function (b) {
        b.addEventListener('click', function () {
          selectGoal(b.dataset.goal);
          go(STEP.PARAMS);
        });
      });
    },

    function bindParams() {
      var amountRange = $('p-amount');
      var amountText = $('p-amount-text');
      var termRange = $('p-term');

      amountRange.addEventListener('input', function () {
        s.amount = AMOUNT_STOPS[+amountRange.value];
        amountText.value = Loans.fmt.number(s.amount);
        updatePreview();
      });
      amountText.addEventListener('input', function () {
        var n = parseInt(amountText.value.replace(/\D/g, ''), 10) || 0;
        amountText.value = n ? Loans.fmt.number(n) : '';
        s.amount = n;
        amountRange.value = nearest(AMOUNT_STOPS, n);
        updatePreview();
      });
      // При уходе с поля подтягиваем сумму в допустимые границы калькулятора
      amountText.addEventListener('change', function () {
        s.amount = Math.min(Math.max(s.amount, Loans.LIMITS.amountMin), Loans.LIMITS.amountMax);
        amountText.value = Loans.fmt.number(s.amount);
        amountRange.value = nearest(AMOUNT_STOPS, s.amount);
        updatePreview();
      });
      termRange.addEventListener('input', function () {
        s.term = TERM_STOPS[+termRange.value];
        $('p-term-text').textContent = fmt.term(s.term);
        updatePreview();
      });
      $('wizard-body').querySelectorAll('[data-collateral]').forEach(function (b) {
        b.addEventListener('click', function () {
          s.collateral = b.dataset.collateral;
          $('wizard-body').querySelectorAll('[data-collateral]').forEach(function (x) {
            x.setAttribute('aria-checked', String(x === b));
          });
          updatePreview();
        });
      });
    },

    function bindOffer() {
      $('wizard-body').querySelectorAll('[data-product]').forEach(function (b) {
        b.addEventListener('click', function () {
          s.productId = b.dataset.product;
          render();
        });
      });
    },

    function bindForm() {
      var body = $('wizard-body');
      DatePicker.enhance(body); // маска даты срабатывает раньше обработчиков ниже
      body.querySelectorAll('[data-field]').forEach(function (el) {
        var path = el.dataset.field;
        var parts = path.split('.');
        if (el.tagName === 'BUTTON') {
          el.addEventListener('click', function () {
            setField(parts[0], parts[1], el.dataset.value === 'true');
            el.parentNode.querySelectorAll('button').forEach(function (x) {
              x.setAttribute('aria-checked', String(x === el));
            });
            clearFieldError(el, path);
          });
          return;
        }
        el.addEventListener('input', function () {
          if (path === 'purpose') {
            s.purpose = el.value;
          } else if (el.dataset.type === 'money') {
            var n = parseInt(el.value.replace(/\D/g, ''), 10);
            el.value = isFinite(n) ? Loans.fmt.number(n) : '';
            setField(parts[0], parts[1], isFinite(n) ? n : '');
          } else if (el.dataset.type === 'date') {
            s.dateRaw[path] = el.value;
            setField(parts[0], parts[1], DatePicker.parse(el.value) || '');
          } else if (el.dataset.type === 'number') {
            var num = parseFloat(el.value.replace(',', '.').replace(/[^\d.]/g, ''));
            setField(parts[0], parts[1], isFinite(num) ? num : '');
          } else {
            setField(parts[0], parts[1], el.value.trim() ? el.value : '');
          }
          if (s.prefilled) delete s.prefilled[path];
          clearFieldError(el, path);
        });
        if (el.tagName === 'SELECT') {
          el.addEventListener('change', function () {
            setField(parts[0], parts[1], el.value);
            clearFieldError(el, path);
          });
        }
      });
    },

    function bindDocs() {
      $('wizard-body').querySelectorAll('[data-doc]').forEach(function (input) {
        input.addEventListener('change', function () {
          if (input.files && input.files[0]) uploadDoc(input.dataset.doc, input.files[0]);
        });
      });
      ['bki', 'pd'].forEach(function (k) {
        $('c-' + k).addEventListener('change', function (e) {
          s.consents[k] = e.target.checked;
          $('wizard-next').disabled = !canContinue();
        });
      });
    },

    function bindReview() {
      $('wizard-body').querySelectorAll('[data-step]').forEach(function (b) {
        b.addEventListener('click', function () {
          go(+b.dataset.step);
        });
      });
    },
  ];

  function clearFieldError(el, path) {
    if (!s.errors[path]) return;
    delete s.errors[path];
    var field = el.closest('.field');
    if (field) {
      field.classList.remove('has-error');
      var msg = field.querySelector('.field-error');
      if (msg) msg.remove();
    }
  }

  // ---------- Переходы ----------

  function go(step) {
    hideError();
    s.step = step;
    if (step === STEP.FORM) prefill();
    render();
    $('view-wizard').scrollIntoView({ block: 'start' });
  }

  async function withBusy(fn) {
    s.busy = true;
    render();
    try {
      return await fn();
    } finally {
      s.busy = false;
    }
  }

  async function next() {
    hideError();

    if (s.step === STEP.OFFER || s.step === STEP.FORM || s.step === STEP.DOCS) {
      if (s.step === STEP.FORM && !validateForm()) {
        render();
        var first = $('wizard-body').querySelector('.has-error input, .has-error textarea, .has-error select, .has-error button');
        if (first) first.focus();
        showError('Заполните отмеченные поля.');
        return;
      }
      var res = await withBusy(save);
      if (!res.ok) {
        render();
        showError(serverError(res));
        return;
      }
      go(s.step + 1);
      return;
    }

    if (s.step === STEP.REVIEW) {
      var result = await withBusy(submit);
      if (result.ok) {
        s.done = true;
        s.changed = true;
        LoanDraft.clear(); // расчёт из калькулятора больше не нужен
        render();
        $('wizard-finish').addEventListener('click', exit);
        return;
      }
      if (result.missing) {
        s.step = STEP.FORM;
        s.errors = {};
        result.missing.forEach(function (f) {
          Catalog.fieldPaths(f).forEach(function (path) { s.errors[path] = 'Заполните поле'; });
        });
        render();
        showError('Банк не принял заявку: не заполнено — ' + result.missing.map(Catalog.fieldLabel).join(', ') + '.');
        return;
      }
      render();
      showError(serverError(result.res));
      return;
    }

    go(s.step + 1);
  }

  // Сохраняем последние изменения и отправляем. Возвращает { ok } | { missing: [...] } | { res }
  // Файл уходит на сервер сразу, как выбран: POST /applications/{id}/documents (multipart: kind, file)
  async function uploadDoc(kind, file) {
    var err = $('doc-error');
    var fail = function (text) {
      err.textContent = text;
      err.hidden = false;
    };
    if (file.size > 10 * 1024 * 1024) return fail('Файл больше 10 МБ — выберите поменьше.');
    if (!s.app) return fail('Сначала сохраните заявку: вернитесь на шаг назад и нажмите «Далее».');
    err.hidden = true;
    var form = new FormData();
    form.append('kind', kind);
    form.append('file', file);
    s.busy = true;
    var res = await api('/applications/' + encodeURIComponent(s.app.id) + '/documents', {
      method: 'POST', form: form, idempotencyKey: api.newKey(),
    });
    s.busy = false;
    if (!res.ok) {
      render();
      fail(res.status === 413 ? 'Файл больше 10 МБ.' : res.status === 400 ? 'Подходят только PDF, JPG и PNG.' : 'Не удалось загрузить файл. Попробуйте ещё раз.');
      return;
    }
    s.files[kind] = res.data.filename || file.name;
    s.changed = true;
    render();
  }

  async function submit() {
    var saved = await save();
    if (!saved.ok) return { res: saved };
    var res = await mutate('/applications/' + encodeURIComponent(s.app.id) + '/submit', 'POST');
    if (res.ok) return { ok: true };
    if (res.status === 422 && res.data && res.data.missing_fields) return { missing: res.data.missing_fields };
    return { res: res };
  }

  function exit() {
    var changed = s && s.changed;
    if (Wizard.onExit) Wizard.onExit(changed);
  }

  $('wizard-next').addEventListener('click', next);
  $('wizard-prev').addEventListener('click', function () {
    if (s.step > 0) go(s.step - 1);
  });
  $('wizard-close').addEventListener('click', exit);
  $('wizard-stepper').addEventListener('click', function (e) {
    var b = e.target.closest('[data-step]');
    if (b) go(+b.dataset.step);
  });

  // ---------- Открытие ----------

  async function openDraft(id) {
    $('wizard-body').innerHTML = '<div class="loading"><div class="spinner spinner-dark" aria-hidden="true"></div><span>Открываем черновик…</span></div>';
    $('wizard-foot').classList.add('hidden');
    var res = await api('/applications/' + encodeURIComponent(id));
    if (!res.ok || res.data.status !== 'DRAFT') {
      $('wizard-body').innerHTML =
        '<div class="empty"><h3>' + (res.ok ? 'Заявка уже отправлена' : 'Не удалось открыть черновик') + '</h3>' +
        '<p>' + (res.ok ? 'Её статус можно посмотреть в кабинете.' : 'Попробуйте ещё раз или начните новую заявку.') + '</p>' +
        '<a href="#new-loan" class="btn btn-primary btn-inline">Новая заявка</a></div>';
      return;
    }
    var a = res.data;
    var front = Catalog.frontId(a.product_id);
    var g = front ? Catalog.goalForProduct(front) : Catalog.GOALS[0];
    var form = a.form || {};
    var consents = form.consents || {};
    delete form.consents;

    s.app = { id: a.id, version: a.version };
    s.savedAt = new Date(a.updated_at);
    s.goalId = g.id;
    s.amount = a.amount || g.amount;
    s.term = a.term_months || g.term;
    s.purpose = a.purpose || g.purpose;
    s.form = Catalog.decomposeForm(form);
    s.consents = { bki: !!consents.bki_request, pd: !!consents.personal_data };
    (a.documents || []).forEach(function (d) { s.files[d.kind] = d.filename; });
    s.collateral = form.applicant && form.applicant.has_collateral ? 'with' : g.collateral === 'with' ? 'with' : 'without';
    s.productId = front;
    go(front ? STEP.FORM : STEP.GOAL);
  }

  window.Wizard = {
    onExit: null,

    open: function (opts) {
      reset(opts.context);
      hideError();
      $('wizard-title').textContent = opts.applicationId ? 'Продолжение заявки' : 'Новый кредит';

      if (opts.applicationId) {
        openDraft(opts.applicationId);
        return;
      }

      // После калькулятора начинаем сразу с предложения: цель, сумма и срок уже выбраны
      var d = opts.calcDraft;
      if (d && Loans.productById(d.productId)) {
        var g = Catalog.goalForProduct(d.productId);
        s.goalId = g.id;
        s.purpose = g.purpose;
        s.amount = d.amount;
        s.term = d.term;
        s.collateral = d.collateral;
        var available = Catalog.isAvailable(Loans.productById(d.productId), query());
        s.productId = available ? d.productId : null;
        s.fromCalc = { lost: !available };
        if (Catalog.recommend(g, query()).length) {
          go(STEP.OFFER);
          return;
        }
        go(STEP.PARAMS);
        return;
      }

      go(STEP.GOAL);
    },
  };
})();
