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
  if (!Auth.requireLogin()) return;

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

  function maskAccount(acc) {
    return acc ? '•••• ' + acc.slice(-4) : '—';
  }

  function row(label, value) {
    return '<div class="kv"><dt>' + esc(label) + '</dt><dd>' + value + '</dd></div>';
  }

  function renderProfile(res) {
    var me = state.me;
    if (!me) {
      $('profile').innerHTML = '<p class="muted">' + esc(errorText(res, 'профиль')) + '</p>';
      $('contacts').innerHTML = '';
      return;
    }
    var c = me.company;
    var u = me.user;

    $('greeting').textContent = greetingWord() + ', ' + firstNames(u.full_name);
    $('greeting-sub').textContent = c.name + ' · ИНН ' + c.inn;
    $('chip-name').textContent = c.name;
    $('chip-inn').textContent = 'ИНН ' + c.inn;
    $('company-chip').classList.remove('hidden');

    $('profile').innerHTML =
      '<div class="profile-name">' + icon('building', 20) + '<span>' + esc(c.name) + '</span></div>' +
      '<dl class="kv-list">' +
      row('ИНН', esc(c.inn)) +
      row(c.org_type === 'IP' ? 'ОГРНИП' : 'ОГРН', esc(c.ogrn)) +
      row('Дата регистрации', esc(fmt.date(c.registration_date)) + '<span class="kv-sub">в бизнесе ' + esc(fmt.age(c.registration_date)) + '</span>') +
      row('ОКВЭД', esc(c.okved) + (c.okved_title ? '<span class="kv-sub">' + esc(c.okved_title) + '</span>' : '')) +
      row(c.org_type === 'IP' ? 'Предприниматель' : 'Руководитель', esc(c.director)) +
      row('Адрес', esc(c.address)) +
      row('Расчётный счёт', '<span title="' + esc(c.account_number) + '">' + esc(maskAccount(c.account_number)) + '</span>') +
      '</dl>' + demoTag('GET', '/me');

    $('contacts').innerHTML =
      '<dl class="kv-list">' +
      row('Представитель', esc(u.full_name) + '<span class="kv-sub">' + esc(u.position) + '</span>') +
      row('Телефон', esc(formatPhone(u.phone))) +
      row('Почта', esc(u.email || '—')) +
      '</dl>';
  }

  // ---------- Сводка ----------

  function tile(label, value, sub, tone, extra) {
    return (
      '<div class="tile' + (tone ? ' tile-' + tone : '') + '">' +
      '<div class="tile-label">' + label + '</div>' +
      '<div class="tile-value">' + value + '</div>' +
      (sub ? '<div class="tile-sub">' + sub + '</div>' : '') +
      (extra || '') +
      '</div>'
    );
  }

  // Шкала долговой нагрузки 0…3: отметки на 1 и 2, всё, что выше 3, упирается в край
  function gauge(ratio) {
    var pos = ratio == null ? 100 : Math.min(ratio / 3, 1) * 100;
    return (
      '<div class="gauge" aria-hidden="true">' +
      '<div class="gauge-track"><span class="g-ok"></span><span class="g-mid"></span><span class="g-high"></span></div>' +
      '<div class="gauge-pin" style="left:' + pos.toFixed(1) + '%"></div>' +
      '<div class="gauge-scale"><span>0</span><span>1</span><span>2</span><span>3+</span></div>' +
      '</div>'
    );
  }

  function renderSummary() {
    var b = state.bootstrap;
    var loans = state.loans ? state.loans.loans : null;
    var html = '';

    // 1. Общий долг (из БКИ — по всем банкам)
    if (b && b.total_debt != null) {
      var ours = loans ? loans.reduce(function (s, l) { return s + l.principal_left; }, 0) : null;
      var sub = 'по всем банкам';
      if (ours === 0) sub = 'весь долг — в других банках';
      else if (ours != null) sub = 'у нас ' + fmt.short(ours) + ' · в других банках ' + fmt.short(Math.max(0, b.total_debt - ours));
      html += tile('Общий долг', fmt.rub(b.total_debt), esc(sub));
    } else {
      html += tile('Общий долг', '—', 'данные ещё не получены');
    }

    // 2. Ежемесячные платежи по нашим кредитам
    if (loans) {
      var monthly = loans.reduce(function (s, l) { return s + l.monthly_payment; }, 0);
      html += tile(
        'Платежи в месяц',
        loans.length ? fmt.rub(monthly) : '0 ₽',
        loans.length ? 'по ' + loans.length + ' ' + UI.plural(loans.length, 'кредиту', 'кредитам', 'кредитам') + ' в нашем банке' : 'кредитов в нашем банке нет'
      );
    } else {
      html += tile('Платежи в месяц', '—', 'не удалось загрузить');
    }

    // 3. Ближайший платёж (просрочка важнее)
    var overdue = loans ? loans.filter(function (l) { return l.status === 'OVERDUE'; }) : [];
    if (overdue.length) {
      var od = overdue[0];
      html += tile(
        'Просроченный платёж',
        fmt.rub(od.overdue_amount),
        'с ' + esc(fmt.dateShort(od.next_payment.date)) + ' · ' + esc(od.product_name),
        'danger'
      );
    } else if (loans && loans.length) {
      var next = loans
        .filter(function (l) { return l.next_payment; })
        .sort(function (a, b) { return a.next_payment.date < b.next_payment.date ? -1 : 1; })[0];
      var days = UI.daysUntil(next.next_payment.date);
      html += tile(
        'Ближайший платёж',
        fmt.rub(next.next_payment.amount),
        esc(fmt.dateShort(next.next_payment.date)) + ', ' + esc(fmt.relDays(next.next_payment.date)),
        days <= 3 ? 'warning' : ''
      );
    } else {
      html += tile('Ближайший платёж', '—', loans ? 'платежей нет' : 'не удалось загрузить');
    }

    // 4. Долговая нагрузка
    if (b && b.total_debt != null) {
      var level = UI.debtLevel(b.debt_to_equity);
      html += tile(
        'Долг / капитал',
        esc(fmt.ratio(b.debt_to_equity)) + ' <span class="level level-' + level.tone + '">' + level.label + '</span>',
        '',
        '',
        gauge(b.debt_to_equity) +
          '<div class="tile-sub" title="' + esc(level.text) + '">капитал ' + esc(fmt.short(b.equity)) + '</div>'
      );
    } else {
      html += tile('Долг / капитал', '—', 'данные ещё не получены');
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

  function renderOffer() {
    var o = state.offer;
    var el = $('offer');
    if (!o || !o.preapproved_limit) {
      el.classList.add('hidden');
      return;
    }
    el.innerHTML =
      '<div class="offer-icon">' + icon('spark', 26) + '</div>' +
      '<div class="offer-text">' +
      '<div class="offer-title">Вам предварительно одобрено до ' + esc(fmt.short(o.preapproved_limit)) + '</div>' +
      '<div class="offer-sub">Ставка от ' + esc(fmt.rate(o.rate_from)) + ' · предложение действует до ' + esc(fmt.dateShort(o.valid_until)) +
      ' · решение быстрее, документов меньше</div>' +
      '</div>' +
      '<a href="#new-loan" class="btn btn-inline offer-btn">Оформить</a>' + demoTag('GET', '/me/offer');
    el.classList.remove('hidden');
  }

  // ---------- Кредиты ----------

  function loanCard(l) {
    var paidShare = Math.max(0, Math.min(1, (l.amount - l.principal_left) / l.amount));
    var days = l.next_payment ? UI.daysUntil(l.next_payment.date) : null;
    var overdue = l.status === 'OVERDUE';
    var soon = !overdue && days != null && days <= 3;

    var nextHtml = l.next_payment
      ? '<div class="loan-next' + (overdue ? ' is-overdue' : soon ? ' is-soon' : '') + '">' +
        '<div class="loan-next-label">' + (overdue ? 'Просрочен платёж' : 'Ближайший платёж') + '</div>' +
        '<div class="loan-next-value">' + fmt.rub(l.next_payment.amount) + '</div>' +
        '<div class="loan-next-date">' + esc(fmt.dateShort(l.next_payment.date)) + ' · ' + esc(fmt.relDays(l.next_payment.date)) + '</div>' +
        '</div>'
      : '';

    return (
      '<article class="loan' + (overdue ? ' loan-overdue' : '') + '">' +
      '<div class="loan-top">' +
      '<div>' +
      '<div class="loan-title">' + esc(l.product_name) + '</div>' +
      '<div class="loan-meta">Договор № ' + esc(l.contract_number) + ' · от ' + esc(fmt.date(l.issued_at)) + '</div>' +
      '</div>' +
      '<span class="badge badge-' + (overdue ? 'danger' : 'success') + '">' + (overdue ? 'Просрочка' : 'Активен') + '</span>' +
      '</div>' +

      '<div class="loan-body">' +
      '<div class="loan-progress">' +
      '<div class="loan-left"><span class="loan-left-value">' + fmt.rub(l.principal_left) + '</span>' +
      '<span class="loan-left-of">осталось из ' + fmt.rub(l.amount) + '</span></div>' +
      '<div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow="' + Math.round(paidShare * 100) + '" aria-label="Выплачено основного долга">' +
      '<span style="width:' + (paidShare * 100).toFixed(1) + '%"></span></div>' +
      '<div class="loan-progress-caption">' +
      '<span>Выплачено ' + Math.round(paidShare * 100) + '% долга · ' + l.payments_made + ' из ' + l.payments_total + ' платежей</span>' +
      '<span>Уплачено всего ' + fmt.rub(l.paid_total) + '</span>' +
      '</div>' +
      '</div>' +
      nextHtml +
      '</div>' +

      '<dl class="loan-facts">' +
      '<div><dt>Ставка</dt><dd>' + fmt.rate(l.rate) + '</dd></div>' +
      '<div><dt>Платёж в месяц</dt><dd>' + fmt.rub(l.monthly_payment) + '</dd></div>' +
      '<div><dt>Срок</dt><dd>' + fmt.term(l.term_months) + '</dd></div>' +
      '<div><dt>Погашение до</dt><dd>' + esc(fmt.date(l.maturity_date)) + '</dd></div>' +
      '</dl>' +

      (overdue
        ? '<div class="notice notice-danger notice-compact">' + icon('alert', 18) +
          '<div>Внесите платёж как можно скорее: за просрочку начисляются пени, и она попадает в кредитную историю.</div></div>'
        : '') +

      '<div class="loan-actions">' +
      '<button type="button" class="btn btn-secondary btn-inline" data-action="schedule" data-loan="' + esc(l.id) + '">График платежей</button>' +
      '<button type="button" class="btn btn-secondary btn-inline" data-action="prepay" data-loan="' + esc(l.id) + '">Погасить досрочно</button>' +
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

  function findLoan(id) {
    var list = (state.loans && state.loans.loans) || [];
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

  function appRow(a) {
    var st = Catalog.status(a.status);
    var draft = a.status === 'DRAFT';
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

  function openDialog(title, html) {
    $('dialog-title').textContent = title;
    $('dialog-body').innerHTML = html;
    if (typeof dialog.showModal === 'function') {
      if (!dialog.open) dialog.showModal();
    } else {
      dialog.setAttribute('open', '');
    }
  }

  function closeDialog() {
    if (typeof dialog.close === 'function') dialog.close();
    else dialog.removeAttribute('open');
  }

  $('dialog-close').addEventListener('click', closeDialog);
  // Клик по затемнению вокруг окна закрывает его
  dialog.addEventListener('click', function (e) {
    if (e.target === dialog) closeDialog();
  });

  // График платежей
  function showSchedule(loan) {
    var statusLabel = { PAID: 'Оплачен', NEXT: 'Ближайший', OVERDUE: 'Просрочен', PLANNED: '' };
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
      'График платежей',
      '<p class="dialog-lead">' + esc(loan.product_name) + ' · договор № ' + esc(loan.contract_number) + '</p>' +
        '<div class="table-wrap"><table class="schedule">' +
        '<thead><tr><th>№</th><th>Дата</th><th class="num">Платёж</th><th class="num">Основной долг</th><th class="num">Проценты</th><th class="num">Остаток</th><th></th></tr></thead>' +
        '<tbody>' + rows + '</tbody></table></div>'
    );
    var current = $('dialog-body').querySelector('.sch-next, .sch-overdue');
    if (current) current.scrollIntoView({ block: 'center' });
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

  // Подробности заявки: GET /applications/{id}
  async function showApplication(id) {
    openDialog('Заявка', '<div class="loading"><div class="spinner spinner-dark" aria-hidden="true"></div><span>Загружаем заявку…</span></div>');
    var res = await api('/applications/' + encodeURIComponent(id));
    if (!res.ok) {
      $('dialog-body').innerHTML = '<p class="muted">' + (res.status === 404 ? 'Заявка не найдена.' : 'Не удалось загрузить заявку. Попробуйте позже.') + '</p>';
      return;
    }
    var a = res.data;
    var st = Catalog.status(a.status);
    var history = (a.history || []).slice().reverse();

    $('dialog-title').textContent = 'Заявка № ' + a.id.slice(0, 8).toUpperCase();
    $('dialog-body').innerHTML =
      '<div class="app-detail-head">' +
      '<div><div class="app-title">' + esc(productName(a.product_id)) + '</div>' +
      '<div class="app-meta">создана ' + esc(fmt.dateTime(a.created_at)) + '</div></div>' +
      '<span class="badge badge-' + st.tone + '">' + esc(st.label) + '</span>' +
      '</div>' +
      stageTrack(a.status) +
      '<div class="notice notice-' + (st.tone === 'danger' ? 'danger' : st.tone === 'warning' ? 'warning' : 'info') + ' notice-compact">' +
      icon('info', 18) + '<div>' + esc(st.text) + '</div></div>' +
      '<dl class="kv-list kv-grid">' +
      row('Сумма', a.amount ? fmt.rub(a.amount) : '—') +
      row('Срок', a.term_months ? fmt.term(a.term_months) : '—') +
      row('Цель', esc(a.purpose || '—')) +
      row('Отправлена', a.submitted_at ? esc(fmt.dateTime(a.submitted_at)) : 'ещё нет') +
      '</dl>' +
      (history.length
        ? '<h3 class="dialog-subtitle">История</h3><ul class="history">' +
          history.map(function (h) {
            return '<li><span class="history-dot"></span><div><div>' + esc(Catalog.status(h.to).label) + '</div>' +
              '<div class="history-at">' + esc(fmt.dateTime(h.at)) + '</div></div></li>';
          }).join('') +
          '<li><span class="history-dot"></span><div><div>Черновик создан</div><div class="history-at">' + esc(fmt.dateTime(a.created_at)) + '</div></div></li>' +
          '</ul>'
        : '') +
      (a.status === 'DRAFT'
        ? '<a class="btn btn-primary" href="#draft=' + encodeURIComponent(a.id) + '" data-close-dialog>Продолжить заполнение</a>'
        : '');

    var link = $('dialog-body').querySelector('[data-close-dialog]');
    if (link) link.addEventListener('click', closeDialog);
  }

  // ---------- Клики по спискам ----------

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

  ready = loadAll();
  route();
})();
