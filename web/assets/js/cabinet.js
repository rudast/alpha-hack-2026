/*
 * cabinet.js — личный кабинет клиента.
 *
 * Адреса страницы:
 *   /personalAccount/              — главная: сводка, кредиты, заявки, профиль компании
 *   /personalAccount/#new-loan     — мастер нового кредита (сюда ведёт вход из калькулятора;
 *                                    если в браузере есть расчёт из калькулятора, мастер начнётся с него)
 *   /personalAccount/#draft=<id>   — продолжить заполнение черновика заявки
 *
 * Данные:
 *   POST /me/bootstrap      — долг, капитал, долговая нагрузка (настоящий бэкенд)
 *   GET  /applications      — заявки клиента (настоящий бэкенд)
 *   GET  /applications/{id} — заявка с историей статусов (настоящий бэкенд)
 *   GET  /me, /loans, /me/offer — пока моки, см. mock-api.js
 */
(function () {
  'use strict';

  // Закрытая страница: без токена сразу на вход.
  if (!Auth.requireLogin('client')) return;

  var $ = function (id) {
    return document.getElementById(id);
  };
  var icon = UI.icon;
  var esc = UI.esc;
  var fmt = UI.fmt;

  var BOOTSTRAP_RETRIES = 3; // /me/bootstrap может ответить 503, пока воркер не получил кредитную историю

  // ---------- Состояние ----------

  var state = {
    bootstrap: null, // { inn, total_debt, equity, debt_to_equity, fetched_at }
    me: null, // { user, company }
    loans: null, // { loans, other_banks }
    offer: null,
    applications: null,
  };

  // ---------- Ошибки ----------

  function showError(text, canRetry) {
    $('error-message').textContent = text;
    $('btn-retry').classList.toggle('hidden', !canRetry);
    $('error-plate').classList.add('visible');
  }

  function hideError() {
    $('error-plate').classList.remove('visible');
  }

  function errorText(res, what) {
    if (res.status === 0) return 'Не удалось связаться с сервером. Проверьте подключение.';
    if (res.status === 403) return 'Этот раздел доступен только клиентам.';
    if (res.status === 404) return 'Не нашли данные вашей компании. Обратитесь в поддержку.';
    return 'Не удалось загрузить ' + what + '. Попробуйте позже.';
  }

  function demoTag(method, path) {
    return api.isMocked && api.isMocked(method, path)
      ? '<span class="demo-tag" title="Метода пока нет на бэкенде, данные из mock-api.js">демо-данные</span>'
      : '';
  }

  // ---------- Загрузка ----------

  function wait(ms) {
    return new Promise(function (resolve) {
      setTimeout(resolve, ms);
    });
  }

  // POST /me/bootstrap. 503 — кредитная история ещё не готова: ждём Retry-After и повторяем.
  async function loadBootstrap() {
    for (var attempt = 1; ; attempt++) {
      var res = await api('/me/bootstrap', { method: 'POST' });
      if (res.status !== 503 || attempt >= BOOTSTRAP_RETRIES) return res;
      var seconds = parseInt(res.headers.get('Retry-After'), 10);
      await wait((seconds > 0 && seconds < 30 ? seconds : 5) * 1000);
    }
  }

  async function loadAll() {
    hideError();

    var b = await loadBootstrap();
    if (b.ok) {
      state.bootstrap = b.data;
    } else {
      showError(
        b.status === 503
          ? 'Данные кредитной истории ещё не пришли. Повторите через несколько секунд.'
          : errorText(b, 'данные о долговой нагрузке'),
        true
      );
    }

    var results = await Promise.all([api('/me'), api('/loans'), api('/me/offer'), api('/applications')]);
    state.me = results[0].ok ? results[0].data : null;
    state.loans = results[1].ok ? results[1].data : null;
    state.offer = results[2].ok ? results[2].data : null;
    state.applications = results[3].ok ? results[3].data || [] : null;

    renderProfile(results[0]);
    renderSummary();
    renderOffer();
    renderLoans(results[1]);
    renderApplications(results[3]);
  }

  async function reloadApplications() {
    var res = await api('/applications');
    state.applications = res.ok ? res.data || [] : null;
    renderApplications(res);
  }

  // ---------- Профиль ----------

  function firstNames(fullName) {
    // «Петров Пётр Петрович» → «Пётр Петрович»
    var parts = String(fullName || '').trim().split(/\s+/);
    return parts.length >= 3 ? parts.slice(1).join(' ') : parts.join(' ');
  }

  function greetingWord() {
    var h = new Date().getHours();
    if (h < 5) return 'Доброй ночи';
    if (h < 12) return 'Доброе утро';
    if (h < 18) return 'Добрый день';
    return 'Добрый вечер';
  }

  function formatPhone(p) {
    var m = /^\+7(\d{3})(\d{3})(\d{2})(\d{2})$/.exec(p || '');
    return m ? '+7 ' + m[1] + ' ' + m[2] + '-' + m[3] + '-' + m[4] : p || '—';
  }

  function row(label, value) {
    return '<div class="kv"><dt>' + esc(label) + '</dt><dd>' + value + '</dd></div>';
  }

  // «ООО «Демо Торг»» → «ДТ», «ИП Иванов Демо» → «ИД»
  function initials(name) {
    var words = String(name || '')
      .replace(/^(ООО|ИП|АО|ПАО|ЗАО)\s+/i, '')
      .replace(/[«»"]/g, '')
      .split(/\s+/)
      .filter(Boolean);
    return (words.slice(0, 2).map(function (w) { return w[0]; }).join('') || '?').toUpperCase();
  }

  function copyBtn(value, label) {
    return '<button type="button" class="copy-btn" data-copy="' + esc(value) + '" aria-label="Скопировать ' + esc(label) + '" title="Скопировать">' +
      icon('copy', 16) + '</button>';
  }

  function prow(label, value, copy) {
    return '<div class="prow"><dt>' + esc(label) + '</dt><dd><span>' + value + '</span>' + (copy || '') + '</dd></div>';
  }

  function renderProfile(res) {
    var me = state.me;
    if (!me) {
      $('drawer-body').innerHTML = '<p class="muted">' + esc(errorText(res, 'профиль')) + '</p>';
      return;
    }
    var c = me.company;
    var u = me.user;
    var ip = c.org_type === 'IP';

    $('greeting').textContent = greetingWord() + ', ' + firstNames(u.full_name);
    $('greeting-sub').textContent = c.name;
    $('chip-avatar').textContent = initials(c.name);
    $('chip-name').textContent = c.name;
    $('chip-inn').textContent = 'ИНН ' + c.inn;
    $('company-chip').classList.remove('hidden');

    $('drawer-body').innerHTML =
      '<div class="drawer-hero">' +
      '<div class="avatar avatar-lg">' + esc(initials(c.name)) + '</div>' +
      '<div>' +
      '<div class="drawer-name">' + esc(c.name) + '</div>' +
      '<div class="drawer-sub">' + (ip ? 'Индивидуальный предприниматель' : 'Общество с ограниченной ответственностью') + '</div>' +
      '</div>' +
      '</div>' +
      '<div class="drawer-badges">' +
      '<span class="pill pill-success">' + icon('check', 14) + 'Действующая</span>' +
      '<span class="pill">в бизнесе ' + esc(fmt.age(c.registration_date)) + '</span>' +
      '<span class="pill">ОКВЭД ' + esc(c.okved) + '</span>' +
      '</div>' +

      '<section class="drawer-section">' +
      '<div class="drawer-section-head"><h3>Реквизиты</h3><span class="source-badge" title="Сведения из государственного реестра, изменить их можно только в ФНС">из ЕГРЮЛ</span></div>' +
      '<dl class="plist">' +
      prow('ИНН', esc(c.inn), copyBtn(c.inn, 'ИНН')) +
      prow(ip ? 'ОГРНИП' : 'ОГРН', esc(c.ogrn), copyBtn(c.ogrn, ip ? 'ОГРНИП' : 'ОГРН')) +
      prow('Дата регистрации', esc(fmt.date(c.registration_date))) +
      prow('Вид деятельности', esc(c.okved_title || c.okved)) +
      prow(ip ? 'Предприниматель' : 'Руководитель', esc(c.director)) +
      prow('Адрес', esc(c.address)) +
      '</dl>' +
      '</section>' +

      '<section class="drawer-section">' +
      '<div class="drawer-section-head"><h3>Расчётный счёт</h3></div>' +
      '<dl class="plist">' +
      prow('Счёт', esc(c.account_number), copyBtn(c.account_number, 'номер счёта')) +
      '</dl>' +
      '</section>' +

      '<section class="drawer-section">' +
      '<div class="drawer-section-head"><h3>Контакты</h3></div>' +
      '<dl class="plist">' +
      prow('Представитель', esc(u.full_name) + '<span class="prow-sub">' + esc(u.position) + '</span>') +
      prow('Телефон', esc(formatPhone(u.phone))) +
      prow('Почта', esc(u.email || '—')) +
      '</dl>' +
      '</section>' +
      demoTag('GET', '/me');
  }

  // ---------- Панель профиля ----------

  var drawer = $('profile-drawer');

  function openDrawer() {
    if (typeof drawer.showModal === 'function') drawer.showModal();
    else drawer.setAttribute('open', '');
  }

  function closeDrawer() {
    if (typeof drawer.close === 'function') drawer.close();
    else drawer.removeAttribute('open');
  }

  $('company-chip').addEventListener('click', openDrawer);
  $('drawer-close').addEventListener('click', closeDrawer);
  drawer.addEventListener('click', function (e) {
    if (e.target === drawer) closeDrawer();
    var btn = e.target.closest('[data-copy]');
    if (!btn || !navigator.clipboard) return;
    navigator.clipboard.writeText(btn.dataset.copy).then(function () {
      btn.classList.add('copied');
      btn.innerHTML = icon('check', 16);
      setTimeout(function () {
        btn.classList.remove('copied');
        btn.innerHTML = icon('copy', 16);
      }, 1500);
    });
  });

  // ---------- Сводка ----------

  function tile(label, value, tone, extra) {
    return (
      '<div class="tile' + (tone ? ' tile-' + tone : '') + '">' +
      '<div class="tile-label">' + label + '</div>' +
      '<div class="tile-value">' + value + '</div>' +
      (extra || '') +
      '</div>'
    );
  }

  // Шкала долговой нагрузки 0…3: зелёная до 1, жёлтая до 2, красная дальше
  function gauge(ratio) {
    var pos = ratio == null ? 100 : Math.min(ratio / 3, 1) * 100;
    return (
      '<div class="gauge" aria-hidden="true">' +
      '<div class="gauge-track"><span class="g-ok"></span><span class="g-mid"></span><span class="g-high"></span></div>' +
      '<div class="gauge-pin" style="left:' + pos.toFixed(1) + '%"></div>' +
      '</div>'
    );
  }

  function chip(text, tone) {
    return '<span class="tile-chip' + (tone ? ' tile-chip-' + tone : '') + '">' + esc(text) + '</span>';
  }

  function renderSummary() {
    var b = state.bootstrap;
    var loans = state.loans ? state.loans.loans : null;
    var html = '';

    // 1. Общий долг (из БКИ — по всем банкам)
    html += tile('Общий долг', b && b.total_debt != null ? fmt.rub(b.total_debt) : '—');

    // 2. Ежемесячные платежи по нашим кредитам
    var monthly = loans ? loans.reduce(function (s, l) { return s + l.monthly_payment; }, 0) : null;
    html += tile('Платежи в месяц', loans ? fmt.rub(monthly) : '—');

    // 3. Ближайший платёж (просрочка важнее)
    var overdue = loans ? loans.filter(function (l) { return l.status === 'OVERDUE'; }) : [];
    if (overdue.length) {
      var od = overdue[0];
      html += tile('Просрочен платёж', fmt.rub(od.overdue_amount) + chip('с ' + fmt.dateShort(od.next_payment.date), 'danger'), 'danger');
    } else if (loans && loans.length) {
      var next = loans
        .filter(function (l) { return l.next_payment; })
        .sort(function (a, b) { return a.next_payment.date < b.next_payment.date ? -1 : 1; })[0];
      var soon = UI.daysUntil(next.next_payment.date) <= 3;
      html += tile('Ближайший платёж', fmt.rub(next.next_payment.amount) + chip(fmt.dateShort(next.next_payment.date), soon ? 'warning' : ''), soon ? 'warning' : '');
    } else {
      html += tile('Ближайший платёж', '—');
    }

    // 4. Долговая нагрузка
    if (b && b.total_debt != null) {
      var level = UI.debtLevel(b.debt_to_equity);
      html += tile(
        'Долг / капитал',
        esc(fmt.ratio(b.debt_to_equity)) + ' <span class="level level-' + level.tone + '">' + level.label + '</span>',
        '',
        gauge(b.debt_to_equity)
      );
    } else {
      html += tile('Долг / капитал', '—');
    }

    $('summary').innerHTML = html;

    // Предупреждение о высокой нагрузке — отдельной плашкой под сводкой
    var warn = $('load-warning');
    if (warn) warn.remove();
    if (b && b.total_debt != null && (b.debt_to_equity == null || b.debt_to_equity > 2)) {
      var lvl = UI.debtLevel(b.debt_to_equity);
      $('summary').insertAdjacentHTML(
        'afterend',
        '<div class="notice notice-danger" id="load-warning">' + icon('alert', 20) +
          '<div><strong>Высокая долговая нагрузка.</strong> ' + esc(lvl.text) +
          ' Снизить её поможет погашение части текущих кредитов.</div></div>'
      );
    }
  }

  // ---------- Предодобренное предложение ----------

  // GET /me/offer: { offer: { id, product_id, product_name, max_amount, term_months, rate_from, valid_until } }
  // или { offer: null, reason }
  function renderOffer() {
    var o = state.offer && state.offer.offer;
    var el = $('offer');
    if (!o || !o.max_amount) {
      el.classList.add('hidden');
      return;
    }
    el.innerHTML =
      '<div class="offer-icon">' + icon('spark', 26) + '</div>' +
      '<div class="offer-text">' +
      '<div class="offer-title">Вам предварительно одобрено до ' + esc(fmt.short(o.max_amount)) + '</div>' +
      '<div class="offer-sub">' + esc(o.product_name) + ' · ставка от ' + esc(fmt.rate(o.rate_from)) +
      ' · предложение действует до ' + esc(fmt.dateShort(o.valid_until)) + ' · решение быстрее, документов меньше</div>' +
      '</div>' +
      '<button type="button" class="btn btn-inline offer-btn" id="offer-btn">Оформить</button>' + demoTag('GET', '/me/offer');
    el.classList.remove('hidden');
    $('offer-btn').addEventListener('click', function () {
      applyOffer(o, this);
    });
  }

  // Заявка по предложению: продукт, сумму и срок сервер берёт из предложения, дальше — обычный черновик
  async function applyOffer(o, button) {
    button.disabled = true;
    var res = await api('/applications', { method: 'POST', json: { offer_id: o.id }, idempotencyKey: api.newKey() });
    button.disabled = false;
    if (res.ok) {
      window.location.hash = '#draft=' + encodeURIComponent(res.data.id);
      reloadApplications();
      return;
    }
    // Предложение истекло или уже использовано — показываем актуальное
    var fresh = await api('/me/offer');
    state.offer = fresh.ok ? fresh.data : null;
    renderOffer();
    showError(state.offer && state.offer.offer ? 'Предложение обновилось — посмотрите новые условия.' : 'Предложение больше не действует.');
  }

  // ---------- Кредиты ----------

  function loanCard(l) {
    var paidShare = Math.max(0, Math.min(1, (l.amount - l.principal_left) / l.amount));
    var overdue = l.status === 'OVERDUE';
    var soon = !overdue && l.next_payment && UI.daysUntil(l.next_payment.date) <= 3;

    return (
      '<article class="loan' + (overdue ? ' loan-overdue' : '') + '">' +
      '<div class="loan-top">' +
      '<div class="loan-title">' + esc(l.product_name) + '</div>' +
      (overdue ? '<span class="badge badge-danger">Просрочка</span>' : '') +
      '</div>' +

      '<div class="loan-left"><span class="loan-left-value">' + fmt.rub(l.principal_left) + '</span>' +
      '<span class="loan-left-of">из ' + fmt.rub(l.amount) + '</span></div>' +
      '<div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow="' + Math.round(paidShare * 100) +
      '" aria-label="Выплачено ' + Math.round(paidShare * 100) + '% основного долга">' +
      '<span style="width:' + (paidShare * 100).toFixed(1) + '%"></span></div>' +

      (l.next_payment
        ? '<div class="loan-next' + (overdue ? ' is-overdue' : soon ? ' is-soon' : '') + '">' +
          '<span class="loan-next-label">' + icon(overdue ? 'alert' : 'calendar', 18) + (overdue ? 'Просрочен с ' : 'Платёж ') +
          esc(fmt.dateShort(l.next_payment.date)) + '</span>' +
          '<strong>' + fmt.rub(l.next_payment.amount) + '</strong>' +
          '</div>'
        : '') +

      '<div class="loan-actions">' +
      '<button type="button" class="btn btn-secondary btn-inline" data-action="schedule" data-loan="' + esc(l.id) + '">' + icon('chart', 18) + 'График платежей</button>' +
      '<button type="button" class="btn btn-ghost btn-inline" data-action="prepay" data-loan="' + esc(l.id) + '">Погасить досрочно</button>' +
      '</div>' +
      '</article>'
    );
  }

  function renderLoans(res) {
    var box = $('loans');
    if (!state.loans) {
      box.innerHTML = '<p class="muted">' + esc(errorText(res, 'кредиты')) + '</p>';
      return;
    }
    var loans = state.loans.loans;
    $('loans-count').innerHTML = (loans.length ? loans.length : '') + demoTag('GET', '/loans');

    if (!loans.length) {
      box.innerHTML =
        '<div class="empty">' +
        '<h3>В нашем банке кредитов пока нет</h3>' +
        '<p>Подберём кредит под задачу бизнеса: ответьте на пару вопросов, а данные о компании мы подставим сами.</p>' +
        '<a href="#new-loan" class="btn btn-primary btn-inline">Подобрать кредит</a>' +
        '</div>';
    } else {
      // Просроченные — первыми
      box.innerHTML = loans
        .slice()
        .sort(function (a, b) { return (b.status === 'OVERDUE') - (a.status === 'OVERDUE'); })
        .map(loanCard)
        .join('');
    }

    renderHistory();

    var others = state.loans.other_banks || [];
    $('other-banks-block').classList.toggle('hidden', !others.length);
    $('other-banks').innerHTML =
      '<div class="other-list">' +
      others
        .map(function (o) {
          return (
            '<div class="other">' +
            '<div class="other-bank">' + esc(o.bank) + '</div>' +
            '<div class="other-kind">' + esc(o.kind) + '</div>' +
            '<div class="other-debt">' + fmt.rub(o.debt) + '</div>' +
            '</div>'
          );
        })
        .join('') +
      '</div>' +
      '<p class="block-note">Эти кредиты учитываются в общем долге и долговой нагрузке. Управлять ими можно только в выдавшем банке.</p>';
  }

  // ---------- Кредитная история (закрытые кредиты) ----------

  var historyOpen = false;

  // «апр 2023»
  var monthYearFmt = new Intl.DateTimeFormat('ru-RU', { month: 'short', year: 'numeric' });

  function monthYear(value) {
    return monthYearFmt.format(UI.parseDate(value)).replace(' г.', '').replace('.', '');
  }

  function closedRow(l) {
    var pills =
      (l.closed_early ? '<span class="pill pill-progress">досрочно на ' + fmt.term(l.months_early) + '</span>' : '') +
      (l.late_payments
        ? '<span class="pill pill-warning">' + l.late_payments + ' ' + UI.plural(l.late_payments, 'просрочка', 'просрочки', 'просрочек') + '</span>'
        : '<span class="pill pill-success">' + icon('check', 14) + 'без просрочек</span>');
    return (
      '<button type="button" class="closed" data-action="schedule" data-loan="' + esc(l.id) + '">' +
      '<span class="closed-icon">' + icon('check', 18) + '</span>' +
      '<span class="closed-main">' +
      '<span class="closed-title">' + esc(l.product_name) + '</span>' +
      '<span class="closed-dates">' + esc(monthYear(l.issued_at)) + ' — ' + esc(monthYear(l.closed_at)) + '</span>' +
      '</span>' +
      '<span class="closed-pills">' + pills + '</span>' +
      '<span class="closed-sum"><strong>' + fmt.rub(l.amount) + '</strong><span>переплата ' + fmt.rub(l.overpayment) + '</span></span>' +
      '<span class="closed-chevron">' + icon('chevron', 18) + '</span>' +
      '</button>'
    );
  }

  function renderHistory() {
    var box = $('loan-history');
    var closed = (state.loans && state.loans.closed_loans) || [];
    box.classList.toggle('hidden', !closed.length);
    if (!closed.length) return;

    var total = closed.reduce(function (sum, l) { return sum + l.amount; }, 0);
    var clean = closed.every(function (l) { return !l.late_payments; });
    box.innerHTML =
      '<button type="button" class="history-toggle" id="history-toggle" aria-expanded="' + historyOpen + '" aria-controls="history-list">' +
      '<span class="history-toggle-icon">' + icon('clock', 20) + '</span>' +
      '<span class="history-toggle-text">' +
      '<span class="history-toggle-title">Кредитная история</span>' +
      '<span class="history-toggle-sub">' + closed.length + ' ' + UI.plural(closed.length, 'закрытый кредит', 'закрытых кредита', 'закрытых кредитов') +
      ' на ' + fmt.short(total) + (clean ? ' · все без просрочек' : '') + '</span>' +
      '</span>' +
      '<span class="history-toggle-chevron">' + icon('chevron', 20) + '</span>' +
      '</button>' +
      '<div class="history-list" id="history-list"' + (historyOpen ? '' : ' hidden') + '>' +
      closed.map(closedRow).join('') +
      '</div>';

    $('history-toggle').addEventListener('click', function () {
      historyOpen = !historyOpen;
      this.setAttribute('aria-expanded', String(historyOpen));
      $('history-list').hidden = !historyOpen;
    });
  }

  function findLoan(id) {
    var list = state.loans ? (state.loans.loans || []).concat(state.loans.closed_loans || []) : [];
    for (var i = 0; i < list.length; i++) if (list[i].id === id) return list[i];
    return null;
  }

  // ---------- Заявки ----------

  function stageTrack(code, compact) {
    var st = Catalog.status(code);
    return (
      '<ol class="stages' + (compact ? ' stages-compact' : '') + ' tone-' + st.tone + '">' +
      Catalog.STAGES.map(function (name, i) {
        var cls = i < st.step ? 'done' : i === st.step ? 'current' : '';
        return '<li class="' + cls + '"><span class="stage-dot"></span><span class="stage-name">' + name + '</span></li>';
      }).join('') +
      '</ol>'
    );
  }

  function productName(id, fallback) {
    var p = Catalog.PRODUCTS[id];
    return fallback || (p ? p.name : 'Продукт не выбран');
  }

  // Кнопка в карточке, когда клиенту нужно ответить банку. Нажатие открывает окно заявки.
  var APP_ACTIONS = {
    INFO_REQUESTED: 'Загрузить документы',
    COUNTER_OFFER: 'Посмотреть условия',
    APPROVED: 'Подписать и получить',
  };

  function appRow(a) {
    var st = Catalog.status(a.status);
    var draft = a.status === 'DRAFT';
    var action = APP_ACTIONS[a.status];
    return (
      '<article class="app" data-app="' + esc(a.id) + '" data-status="' + esc(a.status) + '" tabindex="0" role="button" aria-label="Открыть заявку">' +
      '<div class="app-main">' +
      '<div class="app-title">' + esc(productName(a.product_id, a.product_name)) + '</div>' +
      '<div class="app-meta">' +
      (a.amount ? fmt.rub(a.amount) : 'сумма не указана') +
      (a.term_months ? ' · ' + fmt.term(a.term_months) : '') +
      ' · ' + (a.submitted_at ? 'подана ' + esc(fmt.dateShort(a.submitted_at)) : 'создана ' + esc(fmt.dateShort(a.created_at))) +
      '</div>' +
      '</div>' +
      '<span class="badge badge-' + st.tone + '" data-status="' + esc(a.status) + '">' + esc(st.label) + '</span>' +
      (draft
        ? '<a class="btn btn-primary btn-inline app-continue" href="#draft=' + encodeURIComponent(a.id) + '">Продолжить</a>'
        : action
          ? '<span class="btn btn-primary btn-inline app-continue">' + action + '</span>'
          : '<span class="app-chevron">' + icon('chevron', 18) + '</span>') +
      '<div class="app-stages">' + stageTrack(a.status, true) + '</div>' +
      '</article>'
    );
  }

  function renderApplications(res) {
    var box = $('applications');
    if (!state.applications) {
      box.innerHTML = '<p class="muted">' + esc(errorText(res, 'заявки')) + '</p>';
      return;
    }
    var apps = state.applications.slice().sort(function (a, b) {
      return a.created_at < b.created_at ? 1 : -1;
    });
    $('apps-count').textContent = apps.length || '';
    $('apps-new').classList.toggle('hidden', !apps.length); // в пустом состоянии своя кнопка

    if (!apps.length) {
      box.innerHTML =
        '<div class="empty empty-small">' +
        '<p>Заявок пока нет. Здесь будет видно, на каком этапе рассмотрения ваша заявка.</p>' +
        '<a href="#new-loan" class="btn btn-secondary btn-inline">Подать заявку</a>' +
        '</div>';
      return;
    }
    box.innerHTML = apps.map(appRow).join('');
  }

  // ---------- Окно ----------

  var dialog = $('dialog');

  function openDialog(title, html, wide) {
    $('dialog-title').textContent = title;
    $('dialog-body').innerHTML = html;
    dialog.classList.toggle('dialog-wide', !!wide);
    if (typeof dialog.showModal === 'function') {
      if (!dialog.open) dialog.showModal();
    } else {
      dialog.setAttribute('open', '');
    }
  }

  function closeDialog() {
    openedApp = null;
    if (typeof dialog.close === 'function') dialog.close();
    else dialog.removeAttribute('open');
  }

  $('dialog-close').addEventListener('click', closeDialog);
  // Окно закрыли клавишей Esc — заявка в нём больше не открыта
  dialog.addEventListener('close', function () {
    openedApp = null;
  });
  // Клик по затемнению вокруг окна закрывает его
  dialog.addEventListener('click', function (e) {
    if (e.target === dialog) closeDialog();
  });

  // График платежей: ключевые цифры, график остатка долга, таблица — свёрнутой
  function fact(label, value, sub) {
    return '<div class="fact"><div class="fact-label">' + label + '</div><div class="fact-value">' + value + '</div>' +
      (sub ? '<div class="fact-sub">' + sub + '</div>' : '') + '</div>';
  }

  function showSchedule(loan) {
    var statusLabel = { PAID: 'Оплачен', NEXT: 'Ближайший', OVERDUE: 'Просрочен', PLANNED: '' };
    var paidShare = Math.round(((loan.amount - loan.principal_left) / loan.amount) * 100);
    var overdue = loan.status === 'OVERDUE';
    var closed = loan.status === 'CLOSED';
    var rows = loan.schedule
      .map(function (p) {
        return (
          '<tr class="sch-' + p.status.toLowerCase() + '">' +
          '<td>' + p.number + '</td>' +
          '<td>' + esc(fmt.date(p.date)) + '</td>' +
          '<td class="num">' + fmt.rub(p.amount) + '</td>' +
          '<td class="num">' + fmt.rub(p.principal) + '</td>' +
          '<td class="num">' + fmt.rub(p.interest) + '</td>' +
          '<td class="num">' + fmt.rub(p.balance_after) + '</td>' +
          '<td>' + (statusLabel[p.status] ? '<span class="sch-status">' + statusLabel[p.status] + '</span>' : '') + '</td>' +
          '</tr>'
        );
      })
      .join('');

    openDialog(
      loan.product_name,
      '<p class="dialog-lead">Договор № ' + esc(loan.contract_number) + ' от ' + esc(fmt.date(loan.issued_at)) +
        (closed ? ' · закрыт ' + esc(fmt.date(loan.closed_at)) : '') + '</p>' +
        (closed ? closedNotice(loan) : '') +
        (overdue
          ? '<div class="notice notice-danger notice-compact sched-notice">' + icon('alert', 18) +
            '<div>Платёж ' + fmt.rub(loan.overdue_amount) + ' просрочен с ' + esc(fmt.dateShort(loan.next_payment.date)) +
            '. Внесите его как можно скорее: за просрочку начисляются пени, и она попадает в кредитную историю.</div></div>'
          : '') +
        '<div class="facts">' +
        (closed
          ? fact('Сумма кредита', fmt.rub(loan.amount), 'ставка ' + fmt.rate(loan.rate)) +
            fact('Выплачено всего', fmt.rub(loan.paid_total), loan.payments_made + ' ' + UI.plural(loan.payments_made, 'платёж', 'платежа', 'платежей')) +
            fact('Переплата', fmt.rub(loan.overpayment), 'проценты за ' + fmt.term(loan.payments_made)) +
            fact('Закрыт', esc(fmt.date(loan.closed_at)), loan.closed_early ? 'досрочно' : 'в срок')
          : fact('Осталось выплатить', fmt.rub(loan.principal_left), 'выплачено ' + paidShare + '%') +
            fact('Платёж в месяц', fmt.rub(loan.monthly_payment), loan.payments_made + ' из ' + loan.payments_total + ' внесено') +
            fact('Ставка', fmt.rate(loan.rate), 'сумма ' + fmt.short(loan.amount)) +
            fact('Погашение до', esc(fmt.date(loan.maturity_date)), 'срок ' + fmt.term(loan.term_months))) +
        '</div>' +
        '<div class="sched-chart-head">' +
        '<h3>Остаток долга</h3>' +
        '<ul class="chart-key" aria-label="Обозначения">' +
        '<li><span class="key-dot key-paid"></span>оплачен</li>' +
        (closed
          ? ''
          : '<li><span class="key-dot key-' + (overdue ? 'overdue' : 'next') + '"></span>' + (overdue ? 'просрочен' : 'ближайший') + '</li>' +
            '<li><span class="key-dot key-planned"></span>предстоит</li>') +
        '</ul>' +
        '</div>' +
        '<div id="balance-chart"></div>' +
        '<details class="sched-table">' +
        '<summary>Все платежи таблицей</summary>' +
        '<div class="table-wrap"><table class="schedule">' +
        '<thead><tr><th>№</th><th>Дата</th><th class="num">Платёж</th><th class="num">Основной долг</th><th class="num">Проценты</th><th class="num">Остаток</th><th></th></tr></thead>' +
        '<tbody>' + rows + '</tbody></table></div>' +
        '</details>' +
        (closed
          ? ''
          : '<div class="sched-actions">' +
            '<button type="button" class="btn btn-secondary btn-inline" id="sched-prepay">Погасить досрочно</button>' +
            '</div>'),
      true
    );
    BalanceChart.render($('balance-chart'), loan);
    if (!closed) {
      $('sched-prepay').addEventListener('click', function () {
        showPrepay(loan);
      });
    }
  }

  function closedNotice(loan) {
    if (loan.late_payments) {
      return '<div class="notice notice-warning notice-compact sched-notice">' + icon('info', 18) +
        '<div>Кредит погашен полностью. ' + loan.late_payments + ' ' + UI.plural(loan.late_payments, 'платёж был внесён', 'платежа были внесены', 'платежей были внесены') +
        ' с опозданием — это видно в кредитной истории.</div></div>';
    }
    return '<div class="notice notice-success notice-compact sched-notice">' + icon('check', 18) +
      '<div>Кредит погашен полностью' + (loan.closed_early ? ' — досрочно, на ' + fmt.term(loan.months_early) + ' раньше срока' : ' и в срок') +
      ', без просрочек.</div></div>';
  }

  // Досрочное погашение — заглушка с расчётом, без отправки в банк
  function showPrepay(loan) {
    var monthsLeft = loan.payments_total - loan.payments_made;
    openDialog(
      'Досрочное погашение',
      '<p class="dialog-lead">' + esc(loan.product_name) + ' · остаток долга ' + fmt.rub(loan.principal_left) + '</p>' +
        '<div class="prepay">' +
        (loan.status === 'OVERDUE'
          ? '<div class="notice notice-danger notice-compact prepay-overdue">' + icon('alert', 18) +
            '<div>Сначала банк спишет просроченный платёж ' + fmt.rub(loan.overdue_amount) + ', остаток пойдёт в досрочное погашение.</div></div>'
          : '') +
        '<label class="field-label" for="prepay-amount">Сколько хотите внести</label>' +
        '<div class="money-input"><input type="text" inputmode="numeric" id="prepay-amount" autocomplete="off" /><span>₽</span></div>' +
        '<div class="prepay-quick">' +
        '<button type="button" class="chip" data-prepay="0.25">25%</button>' +
        '<button type="button" class="chip" data-prepay="0.5">50%</button>' +
        '<button type="button" class="chip" data-prepay="1">Весь долг</button>' +
        '</div>' +
        '<div class="prepay-result" id="prepay-result"></div>' +
        '<div class="notice notice-info notice-compact">' + icon('info', 18) +
        '<div>Это предварительный расчёт. Отправка заявки на досрочное погашение появится в следующей версии кабинета.</div></div>' +
        '<button type="button" class="btn btn-primary" disabled>Отправить заявку в банк</button>' +
        '</div>'
    );

    var input = $('prepay-amount');
    function recalc() {
      var value = Math.min(parseMoney(input.value), loan.principal_left);
      var out = $('prepay-result');
      if (!value) {
        out.innerHTML = '';
        return;
      }
      if (value >= loan.principal_left) {
        out.innerHTML = '<div class="prepay-row"><span>Кредит будет закрыт полностью</span><strong>' + fmt.rub(loan.principal_left) + '</strong></div>' +
          '<div class="prepay-hint">Проценты за текущий месяц банк пересчитает на дату погашения.</div>';
        return;
      }
      var left = loan.principal_left - value;
      var newPayment = Loans.annuity(left, loan.rate, monthsLeft);
      // Если платёж оставить прежним, срок сократится
      var r = loan.rate / 100 / 12;
      var newTerm = Math.ceil(-Math.log(1 - (left * r) / loan.monthly_payment) / Math.log(1 + r));
      out.innerHTML =
        '<div class="prepay-row"><span>Уменьшить платёж</span><strong>' + fmt.rub(newPayment) + ' в месяц</strong></div>' +
        '<div class="prepay-hint">вместо ' + fmt.rub(loan.monthly_payment) + ', срок прежний</div>' +
        '<div class="prepay-row"><span>или сократить срок</span><strong>на ' + fmt.term(Math.max(0, monthsLeft - newTerm)) + '</strong></div>' +
        '<div class="prepay-hint">платёж прежний, кредит закроется раньше</div>';
    }
    input.addEventListener('input', function () {
      var n = parseMoney(input.value);
      input.value = n ? Loans.fmt.number(n) : '';
      recalc();
    });
    $('dialog-body').querySelectorAll('[data-prepay]').forEach(function (b) {
      b.addEventListener('click', function () {
        input.value = Loans.fmt.number(Math.round(loan.principal_left * parseFloat(b.dataset.prepay)));
        recalc();
      });
    });
    input.focus();
  }

  function parseMoney(text) {
    var n = parseInt(String(text).replace(/\D/g, ''), 10);
    return isFinite(n) ? n : 0;
  }

  // Подробности заявки: GET /applications/{id}. Если банк ждёт ответа клиента — блок с решением и кнопками.
  var openedApp = null; // id заявки в открытом окне — чтобы обновить его, когда банк что-то изменит

  function rateOf(productId) {
    var front = Catalog.frontId(productId);
    var p = front && Loans.productById(front);
    return p ? p.rate : null;
  }

  function terms(t) {
    var rate = t.rate != null ? t.rate : rateOf(t.product_id);
    return {
      product: productName(t.product_id),
      amount: t.amount,
      term: t.term_months,
      rate: rate,
      payment: rate != null ? Loans.annuity(t.amount, rate, t.term_months) : null,
    };
  }

  function termsList(t, compare) {
    var x = terms(t);
    var c = compare ? terms(compare) : null;
    var line = function (label, value, old) {
      return '<div class="t-row"><span>' + label + '</span><strong>' + value + '</strong>' +
        (old && old !== value ? '<s>' + old + '</s>' : '') + '</div>';
    };
    return (
      line('Кредит', esc(x.product), c && esc(c.product)) +
      line('Сумма', fmt.rub(x.amount), c && fmt.rub(c.amount)) +
      line('Срок', fmt.term(x.term), c && fmt.term(c.term)) +
      (x.rate != null ? line('Ставка', fmt.rate(x.rate), c && c.rate != null ? fmt.rate(c.rate) : null) : '') +
      (x.payment != null ? line('Платёж в месяц', '≈ ' + fmt.rub(x.payment), c && c.payment != null ? '≈ ' + fmt.rub(c.payment) : null) : '')
    );
  }

  // Пояснение сотрудника: при отказе лежит в decision.reason, иначе — в комментарии последнего перехода.
  // В остальных статусах decision.reason пишет воркер для банка, клиенту его не показываем
  function staffComment(a, status) {
    var d = a.decision || {};
    if (status === 'REJECTED' && d.reason) return d.reason;
    var h = (a.history || []).filter(function (e) { return e.to === status && e.comment && e.by !== 'worker'; });
    return h.length ? h[h.length - 1].comment : '';
  }

  // Что просит банк: текст сотрудника в info_request, у воркера — комментарий последнего перехода в INFO_REQUESTED
  function requestText(a) {
    if (a.info_request) return a.info_request;
    var h = (a.history || []).filter(function (e) { return e.to === 'INFO_REQUESTED' && e.comment; });
    return h.length ? h[h.length - 1].comment : '';
  }

  // Поля, которые воркер не смог получить из документов: объясняем, откуда они берутся
  var DATA_HINTS = {
    turnover_monthly: 'Подтверждённый месячный оборот банк считает по бухгалтерской отчётности или выписке по счёту. Приложите такой документ в виде PDF с текстом (не фотографию): по фото цифры прочитать нельзя.',
    monthly_turnovers: 'Нужны помесячные обороты минимум за 6 месяцев — приложите выписку по расчётному счёту (PDF с текстом).',
    collateral_confirmed_value: 'Нужна подтверждённая стоимость залога — приложите отчёт об оценке (строка «Отчёт об оценке» ниже).',
    industry_risk: 'Банк не смог оценить риск вашей отрасли по ОКВЭД. Это внутренняя настройка банка, от вас ничего не требуется: сообщите об этом специалисту поддержки.',
    okved: 'В данных компании не указан ОКВЭД. Это внутренняя настройка банка, от вас ничего не требуется.',
  };

  // Из какого документа воркер берёт недостающие данные: этот документ показываем как запрошенный,
  // даже если его нет в списке документов продукта (бэкенд принимает любой вид из каталога)
  function dataDocuments(field, productId) {
    if (field === 'collateral_confirmed_value') return ['valuation_report'];
    if (field === 'monthly_turnovers') return ['account_statement'];
    if (field === 'turnover_monthly') {
      if (productId === 'working_capital') return ['financial_statements'];
      if (productId === 'marketplace_seller') return ['marketplace_sales_report'];
      return ['account_statement'];
    }
    return [];
  }

  function bankComment(text) {
    return text ? '<div class="bank-comment"><span>Комментарий специалиста</span>' + esc(text) + '</div>' : '';
  }

  function decisionBlock(a) {
    var d = a.decision || {};
    var account = state.me && state.me.company.account_number ? '•••• ' + state.me.company.account_number.slice(-4) : 'расчётный счёт';

    if (a.status === 'APPROVED') {
      return (
        '<section class="decision decision-success">' +
        '<div class="decision-title">' + icon('check', 20) + 'Банк одобрил кредит</div>' +
        '<div class="t-list">' + termsList(d.offer || Object.assign({}, a, { rate: d.rate })) + '</div>' +
        bankComment(staffComment(a, a.status)) +
        '<div class="decision-actions"><button type="button" class="btn btn-primary btn-inline" data-app-action="accept">Подписать и получить</button></div>' +
        '<p class="decision-fine">После подписания деньги поступят на счёт ' + esc(account) + ', кредит появится в «Моих кредитах».</p>' +
        '</section>'
      );
    }

    if (a.status === 'COUNTER_OFFER') {
      return (
        '<section class="decision decision-warning">' +
        '<div class="decision-title">' + icon('info', 20) + 'Банк предлагает другие условия</div>' +
        '<p class="decision-text">Зачёркнуто — то, что вы просили в заявке.</p>' +
        '<div class="t-list">' + termsList(d.offer, a) + '</div>' +
        bankComment(staffComment(a, a.status)) +
        '<div class="decision-actions">' +
        '<button type="button" class="btn btn-primary btn-inline" data-app-action="accept">Принять и подписать</button>' +
        '<button type="button" class="btn btn-secondary btn-inline" data-app-action="decline-ask">Отказаться</button>' +
        '</div>' +
        '<div class="decision-confirm" id="decline-confirm" hidden>' +
        '<p>Заявка вернётся в черновик: вы сможете изменить сумму, срок или вид кредита и подать её снова.</p>' +
        '<div class="decision-actions">' +
        '<button type="button" class="btn btn-secondary btn-inline" data-app-action="decline">Да, отказаться</button>' +
        '<button type="button" class="btn btn-ghost btn-inline" data-app-action="decline-cancel">Отмена</button>' +
        '</div></div>' +
        '</section>'
      );
    }

    if (a.status === 'INFO_REQUESTED') {
      var have = {};
      (a.documents || []).forEach(function (doc) { have[doc.kind] = doc; });
      // Что просит воркер — в decision.missing_fields (documents.<kind>), сотрудник — текстом в info_request
      var kinds = (d.missing_fields || []).filter(function (f) { return f.indexOf('documents.') === 0; }).map(function (f) { return f.slice(10); });
      var otherFields = (d.missing_fields || []).filter(function (f) { return f.indexOf('documents.') !== 0; });
      otherFields.forEach(function (f) {
        dataDocuments(f, a.product_id).forEach(function (k) {
          if (kinds.indexOf(k) === -1) kinds.push(k);
        });
      });
      var asked = {};
      kinds.forEach(function (k) { asked[k] = true; });
      // Документ приложен, но нужных данных банк из него не получил — значит, файл не тот или это фото/скан
      var unreadable = {};
      otherFields.forEach(function (f) {
        dataDocuments(f, a.product_id).forEach(function (k) {
          if (have[k]) unreadable[k] = true;
        });
      });
      // Кроме запрошенного показываем все документы продукта (и необязательные): любой можно приложить или заменить
      var all = Catalog.documentsFor(a.product_id);
      all.required.concat(all.optional).forEach(function (x) {
        if (kinds.indexOf(x.kind) === -1) kinds.push(x.kind);
      });
      (a.documents || []).forEach(function (x) {
        if (kinds.indexOf(x.kind) === -1) kinds.push(x.kind);
      });
      return (
        '<section class="decision decision-warning">' +
        '<div class="decision-title">' + icon('file', 20) + 'Банк запросил данные</div>' +
        (requestText(a) ? '<div class="bank-comment"><span>Что нужно</span>' + esc(requestText(a)) + '</div>' : '') +
        otherFields.map(function (f) {
          return '<p class="decision-text">' + esc(DATA_HINTS[f] || 'Дополните в заявке: ' + Catalog.fieldLabel(f) + '.') + '</p>';
        }).join('') +
        '<ul class="docs req-docs">' +
        kinds.map(function (k) {
          var doc = have[k];
          var bad = unreadable[k];
          var sub = bad ? 'Не удалось прочитать данные из «' + doc.filename + '» — замените файл'
            : doc ? doc.filename : asked[k] ? 'запрошено банком' : 'можно приложить';
          return (
            '<li class="doc' + (bad ? ' unreadable' : doc ? ' attached' : '') + '">' +
            '<span class="doc-icon">' + icon(bad ? 'alert' : doc ? 'check' : 'file', 18) + '</span>' +
            '<span class="doc-text"><span class="doc-title">' + esc(Catalog.documentTitle(k)) + '</span>' +
            '<span class="doc-sub">' + esc(sub) + '</span></span>' +
            '<label class="btn btn-secondary btn-inline doc-btn">' + (doc ? 'Заменить' : 'Прикрепить') +
            '<input type="file" class="visually-hidden" data-req-doc="' + k + '" accept=".pdf,.jpg,.jpeg,.png" /></label>' +
            '</li>'
          );
        }).join('') +
        '</ul>' +
        '<div class="decision-actions">' +
        '<button type="button" class="btn btn-primary btn-inline" data-app-action="resubmit">Отправить в банк</button>' +
        '</div>' +
        '<p class="decision-fine">Приложите нужные документы и отправьте заявку обратно. Она вернётся к специалисту, который её рассматривает.</p>' +
        '</section>'
      );
    }

    if (a.status === 'REJECTED') {
      return (
        '<section class="decision decision-danger">' +
        '<div class="decision-title">' + icon('alert', 20) + 'Банк отказал в кредите</div>' +
        bankComment(staffComment(a, a.status)) +
        '<div class="decision-actions"><a href="#new-loan" class="btn btn-secondary btn-inline" data-close-dialog>Подать новую заявку</a></div>' +
        '</section>'
      );
    }

    if (a.status === 'MANUAL_REVIEW') {
      return (
        '<div class="notice notice-info notice-compact">' + icon('info', 18) + '<div>' +
        (a.assigned_to
          ? 'Заявку рассматривает специалист банка ' + esc(a.assigned_to.full_name) + '.'
          : 'Заявка ждёт специалиста банка.') +
        ' Если понадобятся документы или будет решение, это появится здесь.</div></div>'
      );
    }

    var st = Catalog.status(a.status);
    return '<div class="notice notice-' + (st.tone === 'danger' ? 'danger' : st.tone === 'warning' ? 'warning' : 'info') + ' notice-compact">' +
      icon('info', 18) + '<div>' + esc(st.text) + '</div></div>';
  }

  function historyList(a) {
    var history = (a.history || []).slice().reverse();
    if (!history.length) return '';
    return (
      '<h3 class="dialog-subtitle">История</h3><ul class="history">' +
      history.map(function (h) {
        var docs = h.documents && h.documents.length
          ? '<div class="history-docs">' + esc(h.documents.map(Catalog.documentTitle).join(', ')) + '</div>'
          : '';
        return '<li><span class="history-dot"></span><div><div>' + esc(Catalog.eventLabel(h)) + '</div>' + docs +
          (h.comment ? '<div class="history-comment">' + esc(h.comment) + '</div>' : '') +
          '<div class="history-at">' + esc(fmt.dateTime(h.at)) + (h.by_name ? ' · ' + esc(h.by_name) : '') + '</div></div></li>';
      }).join('') +
      '<li><span class="history-dot"></span><div><div>Черновик создан</div><div class="history-at">' + esc(fmt.dateTime(a.created_at)) + '</div></div></li>' +
      '</ul>'
    );
  }

  async function showApplication(id, quiet) {
    openedApp = id;
    if (!quiet) openDialog('Заявка', '<div class="loading"><div class="spinner spinner-dark" aria-hidden="true"></div><span>Загружаем заявку…</span></div>');
    var res = await api('/applications/' + encodeURIComponent(id));
    if (openedApp !== id) return; // пока грузили, окно закрыли или открыли другое
    if (!res.ok) {
      $('dialog-body').innerHTML = '<p class="muted">' + (res.status === 404 ? 'Заявка не найдена.' : 'Не удалось загрузить заявку. Попробуйте позже.') + '</p>';
      return;
    }
    renderApplication(res.data);
  }

  function renderApplication(a) {
    var st = Catalog.status(a.status);
    $('dialog-title').textContent = 'Заявка № ' + a.id.slice(0, 8).toUpperCase();
    $('dialog-body').innerHTML =
      '<div class="app-detail-head">' +
      '<div><div class="app-title">' + esc(productName(a.product_id)) + '</div>' +
      '<div class="app-meta">создана ' + esc(fmt.dateTime(a.created_at)) + '</div></div>' +
      '<span class="badge badge-' + st.tone + '">' + esc(st.label) + '</span>' +
      '</div>' +
      stageTrack(a.status) +
      '<div class="app-error error-plate" id="app-error" role="alert"><span id="app-error-text"></span></div>' +
      decisionBlock(a) +
      '<dl class="kv-list kv-grid">' +
      row('Сумма', a.amount ? fmt.rub(a.amount) : '—') +
      row('Срок', a.term_months ? fmt.term(a.term_months) : '—') +
      row('Цель', esc(a.purpose || '—')) +
      row('Отправлена', a.submitted_at ? esc(fmt.dateTime(a.submitted_at)) : 'ещё нет') +
      '</dl>' +
      historyList(a) +
      (a.status === 'DRAFT'
        ? '<a class="btn btn-primary" href="#draft=' + encodeURIComponent(a.id) + '" data-close-dialog>Продолжить заполнение</a>'
        : '');

    $('dialog-body').querySelectorAll('[data-close-dialog]').forEach(function (link) {
      link.addEventListener('click', closeDialog);
    });
    $('dialog-body').querySelectorAll('[data-app-action]').forEach(function (b) {
      b.addEventListener('click', function () {
        appAction(a, b.dataset.appAction, b);
      });
    });
    $('dialog-body').querySelectorAll('[data-req-doc]').forEach(function (input) {
      input.addEventListener('change', function () {
        if (input.files && input.files[0]) uploadRequested(a, input.dataset.reqDoc, input.files[0]);
      });
    });
  }

  function appError(text) {
    $('app-error-text').textContent = text;
    $('app-error').classList.add('visible');
  }

  // Файл к запросу банка: POST /applications/{id}/documents (multipart: kind, file)
  async function uploadRequested(a, kind, file) {
    if (file.size > 10 * 1024 * 1024) {
      appError('Файл больше 10 МБ — выберите поменьше.');
      return;
    }
    var form = new FormData();
    form.append('kind', kind);
    form.append('file', file);
    var res = await api('/applications/' + encodeURIComponent(a.id) + '/documents', {
      method: 'POST', form: form, idempotencyKey: api.newKey(),
    });
    if (!res.ok) {
      appError(res.status === 400 ? 'Подходят только PDF, JPG и PNG.' : 'Не удалось загрузить файл. Попробуйте ещё раз.');
      return;
    }
    // Заменили файл — прежние того же вида удаляем: два документа одного вида воркер не читает
    var stale = (a.documents || []).filter(function (d) { return d.kind === kind && d.id !== res.data.id; });
    for (var i = 0; i < stale.length; i++) {
      await api('/applications/' + encodeURIComponent(a.id) + '/documents/' + encodeURIComponent(stale[i].id), {
        method: 'DELETE', idempotencyKey: api.newKey(),
      });
    }
    showApplication(a.id, true);
  }

  async function appAction(a, action, button) {
    if (action === 'decline-ask' || action === 'decline-cancel') {
      $('decline-confirm').hidden = action === 'decline-cancel';
      return;
    }
    var paths = { accept: '/accept', decline: '/decline', resubmit: '/resume' };
    button.disabled = true;
    var res = await api('/applications/' + encodeURIComponent(a.id) + paths[action], {
      method: 'POST', json: {}, idempotencyKey: api.newKey(),
    });
    button.disabled = false;
    if (!res.ok) {
      appError(res.status === 409 ? 'Статус заявки уже изменился — обновите страницу.' : 'Не получилось. Попробуйте ещё раз.');
      return;
    }
    if (action === 'accept') {
      $('dialog-title').textContent = 'Кредит выдан';
      $('dialog-body').innerHTML =
        '<div class="issued">' +
        '<div class="issued-icon">' + icon('check', 34) + '</div>' +
        '<h3>Договор подписан, деньги на счёте</h3>' +
        '<p>' + esc(productName(res.data.product_id)) + ' на ' + fmt.rub(res.data.amount) + ' появился в «Моих кредитах» вместе с графиком платежей.</p>' +
        '<button type="button" class="btn btn-primary btn-inline" id="to-loans">К моим кредитам</button>' +
        '</div>';
      $('to-loans').addEventListener('click', function () {
        closeDialog();
        $('loans-title').scrollIntoView({ behavior: 'smooth', block: 'start' });
      });
      openedApp = null;
      loadAll();
      return;
    }
    renderApplication(res.data);
    reloadApplications();
  }

  // ---------- Клики по спискам ----------

  $('loan-history').addEventListener('click', function (e) {
    var btn = e.target.closest('[data-action="schedule"]');
    var loan = btn && findLoan(btn.dataset.loan);
    if (loan) showSchedule(loan);
  });

  $('loans').addEventListener('click', function (e) {
    var btn = e.target.closest('[data-action]');
    if (!btn) return;
    var loan = findLoan(btn.dataset.loan);
    if (!loan) return;
    if (btn.dataset.action === 'schedule') showSchedule(loan);
    if (btn.dataset.action === 'prepay') showPrepay(loan);
  });

  function openAppFromEvent(e) {
    if (e.target.closest('a')) return; // «Продолжить» — обычная ссылка
    var card = e.target.closest('[data-app]');
    if (card) showApplication(card.dataset.app);
  }

  $('applications').addEventListener('click', openAppFromEvent);
  $('applications').addEventListener('keydown', function (e) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      openAppFromEvent(e);
    }
  });

  // ---------- Маршруты ----------

  // Данные, нужные мастеру: тип компании, возраст бизнеса, текущая нагрузка, счёт
  function wizardContext() {
    var me = state.me;
    var loans = state.loans ? state.loans.loans : [];
    return {
      company: me ? me.company : null,
      user: me ? me.user : null,
      bootstrap: state.bootstrap,
      monthlyPayments: loans.reduce(function (s, l) { return s + l.monthly_payment; }, 0),
    };
  }

  var ready = null; // промис первой загрузки: мастер ждёт данные профиля

  function route() {
    var hash = window.location.hash;
    var draftMatch = /^#draft=(.+)$/.exec(hash);
    var wizardOpen = hash === '#new-loan' || !!draftMatch;

    $('view-overview').classList.toggle('hidden', wizardOpen);
    $('view-wizard').classList.toggle('hidden', !wizardOpen);
    $('btn-new-loan').classList.toggle('hidden', wizardOpen);
    window.scrollTo(0, 0);

    if (wizardOpen) {
      closeDialog();
      ready.then(function () {
        Wizard.open({
          context: wizardContext(),
          applicationId: draftMatch ? decodeURIComponent(draftMatch[1]) : null,
          calcDraft: draftMatch ? null : LoanDraft.get(),
        });
      });
    }
  }

  function goHome() {
    history.pushState(null, '', window.location.pathname);
    route();
  }

  Wizard.onExit = function (changed) {
    goHome();
    if (changed) reloadApplications();
  };

  // ---------- Старт ----------

  $('btn-retry').addEventListener('click', function () {
    ready = loadAll();
  });

  $('btn-logout').addEventListener('click', function () {
    LoanDraft.clear(); // расчёт принадлежит этой сессии
    Auth.clear();
    try {
      sessionStorage.clear();
    } catch (e) {}
    window.location.replace('/login/');
  });

  window.addEventListener('hashchange', route);

  // Банк мог ответить по заявке: обновляем списки и открытое окно, когда вкладку открывают, и раз в 20 секунд
  function refreshFromBank() {
    if (document.hidden) return;
    reloadApplications();
    api('/loans').then(function (res) {
      if (!res.ok) return;
      state.loans = res.data;
      renderSummary();
      renderLoans(res);
    });
    if (openedApp) showApplication(openedApp, true);
  }
  document.addEventListener('visibilitychange', refreshFromBank);
  setInterval(refreshFromBank, 20000);

  ready = loadAll();
  route();
})();
