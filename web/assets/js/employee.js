/*
 * employee.js — кабинет сотрудника банка (/employee/).
 *
 * Очередь спорных заявок, которые скоринг передал на ручную проверку. Сотрудник открывает заявку —
 * она закрепляется за ним («взята в работу»), — смотрит анкету, данные компании и оценку скоринга
 * и принимает решение: одобрить, отклонить с причиной, предложить другие условия, запросить документы
 * или отложить. Всё, кроме «отложить», клиент сразу видит в своём кабинете.
 *
 * Бэкенд:
 *   GET  /employee/me                                — ФИО и должность
 *   GET  /employee/applications[?mine=true]          — общий пул ручной проверки / заявки, взятые мной
 *   GET  /employee/applications/{id}                  — карточка: анкета, компания, скоринг, история
 *   POST /employee/applications/{id}/take             — взять в работу
 *   POST /employee/applications/{id}/release          — вернуть в общий пул
 *   POST /employee/applications/{id}/decision         — { action: approve | reject | request_info | counter_offer, ... }
 *
 * «Отложенные» = заявки, которые я взял и не решил: они остаются за мной (через 2 часа без решения
 * бэкенд сам вернёт их в пул). Списков «Ждут клиента» и «Рассмотренные» бэкенд пока не отдаёт.
 */
(function () {
  'use strict';

  if (!Auth.requireLogin('employee')) return;

  var $ = function (id) {
    return document.getElementById(id);
  };
  var icon = UI.icon;
  var esc = UI.esc;
  var fmt = UI.fmt;

  var state = { items: [], current: null, panel: null, busy: false };

  // Причины отказа одной кнопкой — сотрудник может дописать своё
  var REJECT_REASONS = [
    'Высокая долговая нагрузка',
    'Недостаточная выручка для обслуживания кредита',
    'Негативная кредитная история',
    'Недостоверные сведения в заявке',
    'Залог не покрывает сумму кредита',
  ];

  // Документы, которые можно запросить сверх обязательных для продукта
  var EXTRA_DOCUMENTS = ['account_statement', 'financial_statements', 'egrul_extract', 'title_documents', 'valuation_report', 'business_plan'];

  // ---------- Мелочи ----------

  function showError(text) {
    $('error-message').textContent = text;
    $('error-plate').classList.add('visible');
  }

  function hideError() {
    $('error-plate').classList.remove('visible');
  }

  var toastTimer = null;
  function toast(text) {
    var el = $('toast');
    el.textContent = text;
    el.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () {
      el.hidden = true;
    }, 3500);
  }

  function initials(name) {
    return String(name || '').split(/\s+/).slice(0, 2).map(function (w) { return w[0] || ''; }).join('').toUpperCase();
  }

  // «5 мин», «3 ч», «2 дн.»
  function waited(iso) {
    var mins = Math.max(1, Math.round((Date.now() - new Date(iso).getTime()) / 60000));
    if (mins < 60) return mins + ' мин';
    var hours = Math.round(mins / 60);
    if (hours < 24) return hours + ' ч';
    return Math.round(hours / 24) + ' дн.';
  }

  function scoreTone(score) {
    if (score == null) return 'neutral';
    if (score >= 65) return 'success';
    if (score >= 45) return 'warning';
    return 'danger';
  }

  function orgLabel(type) {
    return type === 'IP' ? 'ИП' : 'ООО';
  }

  function errorText(res) {
    if (res.status === 422 && res.data && res.data.missing_fields) {
      return 'Нельзя принять решение: в заявке не хватает — ' + res.data.missing_fields.map(function (f) {
        return f.indexOf('documents.') === 0 ? Catalog.documentTitle(f.slice(10)) : f;
      }).join(', ') + '.';
    }
    if (res.status === 409 && res.data && res.data.assigned_name) return 'Заявку уже взял ' + res.data.assigned_name + ': обновите список.';
    if (res.status === 0) return 'Не удалось связаться с сервером. Проверьте подключение.';
    if (res.status === 409) return 'Заявку уже изменили: обновите список.';
    if (res.status === 403) return 'Этот раздел доступен только сотрудникам банка.';
    return 'Что-то пошло не так. Попробуйте ещё раз.';
  }

  // ---------- Сотрудник ----------

  async function loadMe() {
    var res = await api('/employee/me');
    if (!res.ok) return;
    $('emp-avatar').textContent = initials(res.data.full_name);
    $('emp-name').textContent = res.data.full_name;
    $('emp-role').textContent = res.data.position;
    $('emp-person').classList.remove('hidden');
  }

  // ---------- Списки ----------

  async function fetchList(mine) {
    var res = await api('/employee/applications' + (mine ? '?mine=true' : ''));
    if (!res.ok) return res;
    res.data = (res.data || []).map(function (a) {
      return {
        id: a.id,
        status: 'MANUAL_REVIEW',
        company: { name: a.company_name, inn: a.company_inn, org_type: String(a.company_inn).length === 12 ? 'IP' : 'LLC' },
        product_id: a.product_id,
        product_name: a.product_name,
        amount: a.amount,
        term_months: a.term_months,
        submitted_at: a.submitted_at,
        queued_at: a.submitted_at,
        assigned_to: a.assigned_to,
        assigned_to_me: mine,
        score: null,
      };
    });
    return res;
  }

  async function loadList() {
    var both = await Promise.all([fetchList(false), fetchList(true)]);
    var bad = both.filter(function (r) { return !r.ok; })[0];
    if (bad) {
      showError(errorText(bad));
      return;
    }
    hideError();
    state.items = both[0].data.concat(both[1].data);
    renderLists();
    // Балл скоринга в списке не приходит — подтягиваем из карточек
    state.items.forEach(function (a) {
      api('/employee/applications/' + encodeURIComponent(a.id)).then(function (r) {
        if (r.ok && r.data.decision && r.data.decision.score != null) {
          a.score = r.data.decision.score;
          if (!state.current) renderLists();
        }
      });
    });
  }

  function row(a) {
    var tone = scoreTone(a.score);
    var flags = '';
    if (a.docs_provided) flags += '<span class="pill pill-progress">' + icon('file', 14) + 'документы досланы</span>';
    if (a.assigned_to_me) flags += '<span class="pill">у вас в работе</span>';
    var st = Catalog.status(a.status);
    var right = a.status === 'MANUAL_REVIEW'
      ? '<span class="q-wait" title="Сколько заявка ждёт в очереди">' + icon('clock', 16) + esc(waited(a.queued_at || a.submitted_at)) + '</span>'
      : '<span class="badge badge-' + st.tone + '">' + esc(st.label) + '</span>';
    return (
      '<button type="button" class="q-row" data-id="' + esc(a.id) + '">' +
      '<span class="q-score q-score-' + tone + '" title="Балл скоринга">' + (a.score != null ? a.score : '—') + '</span>' +
      '<span class="q-main">' +
      '<span class="q-company">' + esc(a.company.name) + '</span>' +
      '<span class="q-meta">' + orgLabel(a.company.org_type) + ' · ИНН ' + esc(a.company.inn) + '</span>' +
      '</span>' +
      '<span class="q-product">' +
      '<span class="q-product-name">' + esc(a.product_name) + '</span>' +
      '<span class="q-meta">' + fmt.rub(a.amount) + ' · ' + fmt.term(a.term_months) + '</span>' +
      '</span>' +
      '<span class="q-flags">' + flags + '</span>' +
      right +
      '<span class="q-chevron">' + icon('chevron', 18) + '</span>' +
      '</button>'
    );
  }

  function byTime(field, desc) {
    return function (a, b) {
      var x = a[field] || '';
      var y = b[field] || '';
      return desc ? (x < y ? 1 : -1) : x < y ? -1 : 1;
    };
  }

  function fold(id, title, items, hint) {
    var box = $(id);
    box.classList.toggle('hidden', !items.length);
    if (!items.length) return;
    var open = box.dataset.open === '1';
    box.innerHTML =
      '<button type="button" class="fold-toggle" aria-expanded="' + open + '">' +
      '<span class="fold-title">' + title + '</span><span class="block-count">' + items.length + '</span>' +
      '<span class="block-hint">' + hint + '</span>' +
      '<span class="fold-chevron">' + icon('chevron', 18) + '</span>' +
      '</button>' +
      '<div class="queue queue-muted"' + (open ? '' : ' hidden') + '>' + items.map(row).join('') + '</div>';
    box.querySelector('.fold-toggle').addEventListener('click', function () {
      var now = box.dataset.open !== '1';
      box.dataset.open = now ? '1' : '';
      this.setAttribute('aria-expanded', String(now));
      box.querySelector('.queue').hidden = !now;
    });
  }

  function renderLists() {
    var items = state.items;
    var review = items.filter(function (a) { return a.status === 'MANUAL_REVIEW'; });
    // В очереди — общий пул; заявки, которые я взял, — ниже, отдельным списком.
    // Заявки, по которым клиент дослал документы, — наверху, дальше — кто дольше ждёт.
    var queue = review
      .filter(function (a) { return !a.assigned_to_me; })
      .sort(function (a, b) {
        return (b.docs_provided - a.docs_provided) || byTime('queued_at')(a, b);
      });
    var postponed = review.filter(function (a) { return a.assigned_to_me; }).sort(byTime('queued_at'));
    var waiting = items.filter(function (a) { return ['INFO_REQUESTED', 'COUNTER_OFFER', 'APPROVED'].indexOf(a.status) !== -1; })
      .sort(byTime('updated_at', true));
    var done = items.filter(function (a) { return ['REJECTED', 'ACTIVE', 'DRAFT', 'CLOSED'].indexOf(a.status) !== -1; })
      .sort(byTime('updated_at', true));

    $('queue-count').textContent = queue.length || '';
    $('queue').innerHTML = queue.length
      ? queue.map(row).join('')
      : '<div class="empty empty-small"><p>Очередь пуста. Новые спорные заявки появятся здесь, как только их передаст скоринг.</p></div>';

    $('postponed-block').classList.toggle('hidden', !postponed.length);
    $('postponed-count').textContent = postponed.length || '';
    $('postponed').innerHTML = postponed.map(row).join('');

    fold('waiting', 'Ждут клиента', waiting, 'запрошены документы или решение ждёт ответа');
    fold('done', 'Рассмотренные', done, 'отказы, выданные кредиты и отказы клиентов');

    $('emp-counters').innerHTML =
      '<span class="counter"><strong>' + queue.length + '</strong> в очереди</span>' +
      '<span class="counter"><strong>' + postponed.length + '</strong> отложено</span>' +
      '<span class="counter"><strong>' + waiting.length + '</strong> ждут клиента</span>';
  }

  // ---------- Окно заявки ----------

  var dialog = $('review');

  function openDialog() {
    if (typeof dialog.showModal === 'function') {
      if (!dialog.open) dialog.showModal();
    } else {
      dialog.setAttribute('open', '');
    }
  }

  function closeDialog() {
    state.current = null;
    state.panel = null;
    if (typeof dialog.close === 'function') dialog.close();
    else dialog.removeAttribute('open');
  }

  $('review-close').addEventListener('click', closeDialog);
  dialog.addEventListener('close', function () {
    state.current = null;
    state.panel = null;
  });

  async function openApp(id) {
    hideError();
    var summary = state.items.filter(function (a) { return a.id === id; })[0];
    $('review-title').textContent = summary ? summary.company.name : 'Заявка';
    $('review-sub').textContent = '';
    $('review-body').innerHTML = '<div class="loading"><div class="spinner spinner-dark" aria-hidden="true"></div><span>Открываем заявку…</span></div>';
    $('review-foot').innerHTML = '';
    openDialog();

    // Открыть заявку на ручной проверке — значит взять её в работу
    var res = summary && summary.status === 'MANUAL_REVIEW' && !summary.assigned_to_me
      ? await api('/employee/applications/' + encodeURIComponent(id) + '/take', { method: 'POST', json: {}, idempotencyKey: api.newKey() })
      : await api('/employee/applications/' + encodeURIComponent(id));
    if (!res.ok) {
      $('review-body').innerHTML = '<p class="muted">' + esc(errorText(res)) + '</p>';
      return;
    }
    state.current = res.data;
    state.panel = null;
    renderApp();
    loadList();
  }

  function kv(label, value) {
    return '<div class="kv"><dt>' + esc(label) + '</dt><dd>' + value + '</dd></div>';
  }

  function displayValue(f, v) {
    if (v === undefined || v === null || v === '') return '<span class="muted">не указано</span>';
    if (f.type === 'bool') return v ? 'Да' : 'Нет';
    if (f.type === 'money') return fmt.rub(v);
    if (f.type === 'select') return esc(Catalog.OPTION_LABELS[v] || v);
    if (f.type === 'date') return esc(fmt.date(v));
    return esc(v);
  }

  function formSection(a) {
    var groups = Catalog.groupsFor(a.product_id);
    return groups.map(function (g) {
      var block = (a.form || {})[g.name] || {};
      return (
        '<div class="r-sub">' + esc(g.title) + '</div>' +
        '<dl class="kv-list">' +
        g.fields.map(function (f) { return kv(f.label, displayValue(f, block[f.key])); }).join('') +
        '</dl>'
      );
    }).join('');
  }

  function documentsSection(a) {
    var need = Catalog.documentsFor(a.product_id).required.filter(function (d) { return d.kind !== 'bki_consent'; });
    var have = {};
    (a.documents || []).forEach(function (d) { have[d.kind] = d; });
    var kinds = need.map(function (d) { return d.kind; });
    (a.documents || []).forEach(function (d) { if (kinds.indexOf(d.kind) === -1) kinds.push(d.kind); });
    return (
      '<ul class="r-docs">' +
      kinds.map(function (k) {
        var d = have[k];
        return (
          '<li class="' + (d ? 'is-attached' : 'is-missing') + '">' +
          '<span class="r-doc-icon">' + icon(d ? 'check' : 'alert', 16) + '</span>' +
          '<span class="r-doc-text"><span>' + esc(Catalog.documentTitle(k)) + '</span>' +
          '<span class="r-doc-sub">' + (d ? esc(d.filename) : 'не приложен') + '</span></span>' +
          '</li>'
        );
      }).join('') +
      '</ul>'
    );
  }

  function historySection(a) {
    var list = (a.history || []).slice().reverse();
    return (
      '<ul class="history">' +
      list.map(function (e) {
        var who = e.by_name ? e.by_name : e.by === 'client' ? 'клиент' : '';
        return (
          '<li><span class="history-dot"></span><div>' +
          '<div>' + esc(Catalog.eventLabel(e)) + (e.client_visible === false ? ' <span class="pill">видно только банку</span>' : '') + '</div>' +
          (e.comment ? '<div class="history-comment">' + esc(e.comment) + '</div>' : '') +
          '<div class="history-at">' + esc(fmt.dateTime(e.at)) + (who ? ' · ' + esc(who) : '') + '</div>' +
          '</div></li>'
        );
      }).join('') +
      '</ul>'
    );
  }

  // Шкала балла 0–100 с порогами «встречное предложение» и «одобрение»
  // Коды воркера предварительной проверки (балл он пока не считает)
  var CHECK_CODES = {
    DEMO_HIGH_DEBT_TO_EQUITY: 'Долг больше собственного капитала более чем в 2 раза',
    CONTENT_UNAVAILABLE: 'Содержимое документа воркеру недоступно — проверьте файл вручную',
    BKI_CONSENT_NOT_VERIFIED: 'Согласие на запрос в БКИ не проверено',
    PASSPORT_NOT_VERIFIED: 'Паспорт не проверен',
  };

  function checkText(code) {
    return CHECK_CODES[code] || code;
  }

  // Балла нет — показываем итог предварительной проверки: флаги риска и что проверить вручную
  function precheckCard(a) {
    var d = a.decision || {};
    var c = a.company || {};
    var after = c.equity > 0 ? (c.total_debt + a.amount) / c.equity : null;
    var flags = d.risk_flags || [];
    var seen = {};
    var review = (d.review_reasons || []).map(function (r) { return r.reason; }).filter(function (code) {
      if (seen[code]) return false;
      seen[code] = true;
      return true;
    });
    var list = function (items, cls, title) {
      if (!items.length) return '';
      return '<div class="sc-reasons ' + cls + '"><div class="sc-reasons-title">' + title + '</div><ul>' +
        items.map(function (code) { return '<li>' + esc(checkText(code)) + '</li>'; }).join('') + '</ul></div>';
    };
    return (
      '<section class="score-card">' +
      '<div class="sc-head"><span>Предварительная проверка</span><span class="sc-model">воркер</span></div>' +
      '<div class="sc-verdict sc-' + (flags.length ? 'warning' : 'success') + '">Балл не рассчитан — решение за специалистом</div>' +
      '<dl class="sc-metrics">' +
      '<div><dt>Долг / капитал сейчас</dt><dd>' + fmt.ratio(c.debt_to_equity != null ? c.debt_to_equity : d.debt_to_equity) + '</dd></div>' +
      '<div><dt>После кредита</dt><dd>' + (after != null ? fmt.ratio(after) : '—') + '</dd></div>' +
      '</dl>' +
      list(flags, 'sc-neg', 'Флаги риска') +
      list(review, 'sc-neg', 'Проверьте вручную') +
      '</section>'
    );
  }

  function scoreCard(a) {
    var d = a.decision || {};
    if (d.score == null) return precheckCard(a);
    var t = d.thresholds || { approve: 65, counter: 45 };
    var tone = scoreTone(d.score);
    var verdict = d.score >= t.approve ? 'Модель одобрила бы' : d.score >= t.counter ? 'Пограничный случай' : 'Модель отказала бы';
    var c = a.company || {};
    var after = c.equity > 0 ? (c.total_debt + a.amount) / c.equity : null;
    var reasons = function (list, cls, title) {
      if (!list || !list.length) return '';
      return '<div class="sc-reasons ' + cls + '"><div class="sc-reasons-title">' + title + '</div><ul>' +
        list.map(function (r) { return '<li>' + esc(r.text) + '</li>'; }).join('') + '</ul></div>';
    };
    return (
      '<section class="score-card">' +
      '<div class="sc-head"><span>Кредитный скоринг</span><span class="sc-model">' + esc(d.model_version || '') + '</span></div>' +
      '<div class="sc-score"><span class="sc-value sc-' + tone + '">' + (d.score != null ? d.score : '—') + '</span><span class="sc-of">из 100</span></div>' +
      '<div class="sc-verdict sc-' + tone + '">' + verdict + '</div>' +
      '<div class="sc-bar" aria-hidden="true">' +
      '<span class="sc-zone sc-zone-bad" style="width:' + t.counter + '%"></span>' +
      '<span class="sc-zone sc-zone-mid" style="width:' + (t.approve - t.counter) + '%"></span>' +
      '<span class="sc-zone sc-zone-ok" style="width:' + (100 - t.approve) + '%"></span>' +
      '<span class="sc-pin" style="left:' + Math.max(0, Math.min(100, d.score || 0)) + '%"></span>' +
      '</div>' +
      '<div class="sc-scale"><span>0</span><span style="left:' + t.counter + '%">' + t.counter + '</span><span style="left:' + t.approve + '%">' + t.approve + '</span><span>100</span></div>' +
      '<dl class="sc-metrics">' +
      '<div><dt>Долг / капитал сейчас</dt><dd>' + fmt.ratio(c.debt_to_equity) + '</dd></div>' +
      '<div><dt>После кредита</dt><dd>' + (after != null ? fmt.ratio(after) : '—') + '</dd></div>' +
      '</dl>' +
      reasons(d.reasons_negative, 'sc-neg', 'Что снижает балл') +
      reasons(d.reasons_positive, 'sc-pos', 'Что повышает балл') +
      '</section>'
    );
  }

  function statusNotice(a) {
    var d = a.decision || {};
    if (a.status === 'MANUAL_REVIEW') return '';
    var st = Catalog.status(a.status);
    var text = st.text;
    if (a.status === 'INFO_REQUESTED') {
      var missing = (d.missing_fields || []).map(function (f) { return f.indexOf('documents.') === 0 ? Catalog.documentTitle(f.slice(10)) : Catalog.fieldLabel(f); });
      text = 'Ждём от клиента: ' + (a.info_request || missing.join(', ') || 'дополнительные данные') + '.';
    } else if (a.status === 'COUNTER_OFFER' && d.offer) {
      text = 'Клиенту предложено: ' + Catalog.PRODUCTS[d.offer.product_id].name + ', ' + fmt.rub(d.offer.amount) + ' на ' + fmt.term(d.offer.term_months) + ' под ' + fmt.rate(d.offer.rate) + '. Ждём ответа.';
    } else if (a.status === 'REJECTED') {
      text = 'Отказ: ' + (d.reason || 'причина не указана');
    }
    return '<div class="notice notice-' + (st.tone === 'danger' ? 'danger' : st.tone === 'success' ? 'success' : st.tone === 'warning' ? 'warning' : 'info') +
      ' notice-compact r-notice">' + icon('info', 18) + '<div><strong>' + esc(st.label) + '.</strong> ' + esc(text) + '</div></div>';
  }

  function renderApp() {
    var a = state.current;
    var c = a.company || {};
    var app = c.applicant || {};
    var payment = Loans.annuity(a.amount, rateOf(a.product_id), a.term_months);

    $('review-title').textContent = c.name;
    $('review-sub').innerHTML =
      esc(orgLabel(c.org_type)) + ' · ИНН ' + esc(c.inn) + ' · заявка № ' + esc(a.id.slice(0, 8).toUpperCase()) +
      ' · отправлена ' + esc(fmt.dateTime(a.submitted_at)) +
      ' <span class="badge badge-' + Catalog.status(a.status).tone + '">' + esc(Catalog.status(a.status).label) + '</span>';

    $('review-body').innerHTML =
      statusNotice(a) +
      '<div class="review-grid">' +
      '<div class="review-main">' +

      '<section class="r-section r-request">' +
      '<div class="r-request-product">' + esc(a.product_name) + '</div>' +
      '<div class="r-request-sum">' + fmt.rub(a.amount) + '<span> на ' + fmt.term(a.term_months) + '</span></div>' +
      '<div class="r-request-meta">ставка по каталогу ' + fmt.rate(rateOf(a.product_id)) + ' · платёж ≈ ' + fmt.rub(payment) + ' в месяц</div>' +
      '<div class="r-purpose">«' + esc(a.purpose || 'цель не указана') + '»</div>' +
      '</section>' +

      '<section class="r-section"><h3>Анкета клиента</h3>' + formSection(a) + '</section>' +

      '<section class="r-section"><h3>Компания <span class="source-badge">из ЕГРЮЛ и БКИ</span></h3>' +
      '<dl class="kv-list">' +
      kv('ОГРН', esc(c.ogrn || '—')) +
      kv('Регистрация', esc(fmt.date(c.registration_date)) + '<span class="kv-sub">в бизнесе ' + esc(fmt.age(c.registration_date)) + '</span>') +
      kv('ОКВЭД', esc(c.okved || '—') + (c.okved_title ? '<span class="kv-sub">' + esc(c.okved_title) + '</span>' : '')) +
      kv(c.org_type === 'IP' ? 'Предприниматель' : 'Руководитель', esc(c.director || '—')) +
      kv('Адрес', esc(c.address || '—')) +
      kv('Общий долг', fmt.rub(c.total_debt)) +
      kv('Собственный капитал', fmt.rub(c.equity)) +
      kv('Просрочки сейчас', c.has_overdue ? '<span class="pill pill-warning">есть просроченный платёж</span>' : 'нет') +
      '</dl></section>' +

      '<section class="r-section"><h3>Заявитель</h3><dl class="kv-list">' +
      kv('ФИО', esc(app.full_name || '—') + (app.position ? '<span class="kv-sub">' + esc(app.position) + '</span>' : '')) +
      kv('Телефон', esc(app.phone || '—')) +
      kv('Почта', esc(app.email || '—')) +
      '</dl></section>' +

      '<section class="r-section"><h3>Документы</h3>' + documentsSection(a) + '</section>' +
      '<section class="r-section"><h3>История</h3>' + historySection(a) + '</section>' +
      '</div>' +

      '<aside class="review-side">' + scoreCard(a) + '</aside>' +
      '</div>';

    renderFoot();
  }

  function rateOf(productId) {
    var front = Catalog.frontId(productId);
    var p = front && Loans.productById(front);
    return p ? p.rate : 18;
  }

  // ---------- Действия ----------

  function renderFoot() {
    var a = state.current;
    var foot = $('review-foot');
    if (a.status !== 'MANUAL_REVIEW') {
      foot.innerHTML = '<div class="action-bar"><span class="muted">Решение по заявке принято, действий не требуется.</span>' +
        '<button type="button" class="btn btn-secondary btn-inline" data-close>Закрыть</button></div>';
      foot.querySelector('[data-close]').addEventListener('click', closeDialog);
      return;
    }

    var panel = state.panel ? panelHtml(state.panel) : '';
    foot.innerHTML =
      (panel ? '<div class="action-panel">' + panel + '</div>' : '') +
      '<div class="action-bar">' +
      '<div class="action-group">' +
      btn('docs', 'Запросить документы', 'file') +
      btn('postpone', 'Отложить', 'clock') +
      '</div>' +
      '<div class="action-group">' +
      btn('reject', 'Отклонить', null, 'btn-danger') +
      btn('counter', 'Предложить другие условия', null, '') +
      btn('approve', 'Одобрить', 'check', 'btn-primary') +
      '</div>' +
      '</div>';

    foot.querySelectorAll('[data-panel]').forEach(function (b) {
      b.addEventListener('click', function () {
        state.panel = state.panel === b.dataset.panel ? null : b.dataset.panel;
        renderFoot();
        var first = foot.querySelector('.action-panel textarea, .action-panel select, .action-panel input');
        if (first) first.focus();
      });
    });
    if (state.panel) bindPanel(state.panel);
  }

  function btn(panel, label, iconName, cls) {
    var active = state.panel === panel;
    return '<button type="button" class="btn btn-inline ' + (cls || 'btn-secondary') + (active ? ' is-active' : '') + '" data-panel="' + panel + '" aria-expanded="' + active + '">' +
      (iconName ? icon(iconName, 18) : '') + label + '</button>';
  }

  function panelHtml(kind) {
    var a = state.current;
    var cancel = '<button type="button" class="btn btn-secondary btn-inline" data-cancel>Отмена</button>';

    if (kind === 'approve') {
      return (
        '<div class="ap-title">Одобрить ' + fmt.rub(a.amount) + ' на ' + fmt.term(a.term_months) + ' под ' + fmt.rate(rateOf(a.product_id)) + '?</div>' +
        '<p class="ap-text">Клиент увидит «Одобрена» и кнопку «Подписать и получить».</p>' +
        '<label class="field-label" for="ap-comment">Комментарий для клиента <span class="optional">необязательно</span></label>' +
        '<textarea class="text-input" id="ap-comment" rows="2"></textarea>' +
        '<div class="ap-actions">' + cancel + '<button type="button" class="btn btn-primary btn-inline" data-submit>Одобрить</button></div>'
      );
    }

    if (kind === 'reject') {
      return (
        '<div class="ap-title">Отклонить заявку</div>' +
        '<p class="ap-text">Причину увидит клиент. Выберите готовую или напишите свою.</p>' +
        '<div class="ap-chips">' + REJECT_REASONS.map(function (r) { return '<button type="button" class="chip" data-reason="' + esc(r) + '">' + esc(r) + '</button>'; }).join('') + '</div>' +
        '<label class="field-label" for="ap-comment">Причина отказа</label>' +
        '<textarea class="text-input" id="ap-comment" rows="2" placeholder="Например: долговая нагрузка после кредита превысит 4"></textarea>' +
        '<div class="ap-error" id="ap-error" hidden></div>' +
        '<div class="ap-actions">' + cancel + '<button type="button" class="btn btn-danger btn-inline" data-submit>Отклонить</button></div>'
      );
    }

    if (kind === 'counter') {
      var org = (a.company || {}).org_type || 'OOO';
      var options = Object.keys(Catalog.PRODUCTS).filter(function (id) {
        var p = Catalog.PRODUCTS[id];
        return !p.orgTypes || p.orgTypes.indexOf(org) !== -1;
      }).map(function (id) {
        return '<option value="' + id + '"' + (id === a.product_id ? ' selected' : '') + '>' + esc(Catalog.PRODUCTS[id].name) + '</option>';
      }).join('');
      return (
        '<div class="ap-title">Предложить другие условия</div>' +
        '<p class="ap-text">Клиент увидит новые условия рядом с запрошенными и сможет принять их или отказаться.</p>' +
        '<div class="ap-grid">' +
        '<div class="ap-field ap-wide"><label class="field-label" for="ap-product">Вид кредита</label><select class="text-input" id="ap-product">' + options + '</select></div>' +
        '<div class="ap-field"><label class="field-label" for="ap-amount">Сумма</label><div class="money-input"><input type="text" inputmode="numeric" id="ap-amount" value="' + Loans.fmt.number(a.amount) + '" /><span>₽</span></div></div>' +
        '<div class="ap-field"><label class="field-label" for="ap-term">Срок, мес.</label><input type="text" inputmode="numeric" class="text-input" id="ap-term" value="' + a.term_months + '" /></div>' +
        '</div>' +
        '<div class="ap-calc" id="ap-calc"></div>' +
        '<label class="field-label" for="ap-comment">Пояснение для клиента</label>' +
        '<textarea class="text-input" id="ap-comment" rows="2" placeholder="Например: с учётом текущей нагрузки можем выдать меньшую сумму на более долгий срок"></textarea>' +
        '<div class="ap-error" id="ap-error" hidden></div>' +
        '<div class="ap-actions">' + cancel + '<button type="button" class="btn btn-primary btn-inline" data-submit>Отправить клиенту</button></div>'
      );
    }

    if (kind === 'docs') {
      var have = (a.documents || []).map(function (d) { return d.kind; });
      var required = Catalog.documentsFor(a.product_id).required.map(function (d) { return d.kind; }).filter(function (k) { return k !== 'bki_consent'; });
      var kinds = required.concat(EXTRA_DOCUMENTS.filter(function (k) { return required.indexOf(k) === -1; }));
      return (
        '<div class="ap-title">Запросить документы у клиента</div>' +
        '<p class="ap-text">Заявка уйдёт в «Ждут клиента». Когда клиент дошлёт документы, она вернётся к вам наверх очереди.</p>' +
        '<div class="ap-docs">' +
        kinds.map(function (k) {
          var attached = have.indexOf(k) !== -1;
          var missing = !attached && required.indexOf(k) !== -1;
          return '<label class="check"><input type="checkbox" value="' + k + '"' + (missing ? ' checked' : '') + ' />' +
            '<span>' + esc(Catalog.documentTitle(k)) + (attached ? ' <span class="pill pill-success">уже приложен</span>' : missing ? ' <span class="pill pill-warning">не приложен</span>' : '') + '</span></label>';
        }).join('') +
        '</div>' +
        '<label class="field-label" for="ap-comment">Комментарий для клиента</label>' +
        '<textarea class="text-input" id="ap-comment" rows="2" placeholder="Например: выписка нужна за последние 6 месяцев"></textarea>' +
        '<div class="ap-error" id="ap-error" hidden></div>' +
        '<div class="ap-actions">' + cancel + '<button type="button" class="btn btn-primary btn-inline" data-submit>Запросить</button></div>'
      );
    }

    // postpone
    return (
      '<div class="ap-title">Отложить заявку</div>' +
      '<p class="ap-text">Заявка останется за вами в списке «Отложенные» под очередью. Клиент этого не увидит.</p>' +
      '<label class="field-label" for="ap-comment">Заметка для себя <span class="optional">необязательно</span></label>' +
      '<textarea class="text-input" id="ap-comment" rows="2" placeholder="Например: жду ответ службы безопасности"></textarea>' +
      '<div class="ap-actions">' + cancel + '<button type="button" class="btn btn-primary btn-inline" data-submit>Отложить</button></div>'
    );
  }

  function parseNum(v) {
    var n = parseInt(String(v).replace(/\D/g, ''), 10);
    return isFinite(n) ? n : 0;
  }

  function counterValues() {
    var product = $('ap-product').value;
    return { product_id: product, amount: parseNum($('ap-amount').value), term_months: parseNum($('ap-term').value), rate: rateOf(product) };
  }

  function updateCalc() {
    var o = counterValues();
    var a = state.current;
    if (!o.amount || !o.term_months) {
      $('ap-calc').innerHTML = '';
      return;
    }
    var pay = Loans.annuity(o.amount, o.rate, o.term_months);
    var was = Loans.annuity(a.amount, rateOf(a.product_id), a.term_months);
    var c = a.company || {};
    var after = c.equity > 0 ? (c.total_debt + o.amount) / c.equity : null;
    $('ap-calc').innerHTML =
      '<div><span>Ставка</span><strong>' + fmt.rate(o.rate) + '</strong></div>' +
      '<div><span>Платёж в месяц</span><strong>' + fmt.rub(pay) + '</strong><em>было ' + fmt.rub(was) + '</em></div>' +
      '<div><span>Долг / капитал после</span><strong>' + (after != null ? fmt.ratio(after) : '—') + '</strong></div>';
  }

  function panelError(text) {
    var el = $('ap-error');
    if (!el) return;
    el.textContent = text;
    el.hidden = !text;
  }

  function bindPanel(kind) {
    var foot = $('review-foot');
    foot.querySelector('[data-cancel]').addEventListener('click', function () {
      state.panel = null;
      renderFoot();
    });
    foot.querySelectorAll('[data-reason]').forEach(function (chip) {
      chip.addEventListener('click', function () {
        $('ap-comment').value = chip.dataset.reason;
        panelError('');
      });
    });
    if (kind === 'counter') {
      ['ap-product', 'ap-amount', 'ap-term'].forEach(function (id) {
        $(id).addEventListener('input', function () {
          if (id === 'ap-amount') {
            var n = parseNum(this.value);
            this.value = n ? Loans.fmt.number(n) : '';
          }
          updateCalc();
        });
      });
      updateCalc();
    }
    foot.querySelector('[data-submit]').addEventListener('click', function () {
      submitPanel(kind, this);
    });
  }

  async function submitPanel(kind, button) {
    if (state.busy) return;
    var a = state.current;
    var comment = ($('ap-comment') && $('ap-comment').value.trim()) || '';
    var path = '/employee/applications/' + encodeURIComponent(a.id);
    var body;
    var done;

    if (kind === 'approve') {
      path += '/decision';
      body = { action: 'approve', reason: comment };
      done = 'Заявка одобрена — клиент увидит кнопку «Подписать и получить»';
    } else if (kind === 'reject') {
      if (!comment) return panelError('Укажите причину отказа — её увидит клиент.');
      path += '/decision';
      body = { action: 'reject', reason: comment };
      done = 'Отказ отправлен клиенту';
    } else if (kind === 'counter') {
      var o = counterValues();
      if (!o.amount || !o.term_months) return panelError('Укажите сумму и срок.');
      var p = Catalog.PRODUCTS[o.product_id];
      if (o.term_months < p.term[0] || o.term_months > p.term[1]) return panelError('Срок для этого продукта — от ' + p.term[0] + ' до ' + p.term[1] + ' мес.');
      if (o.product_id === a.product_id && o.amount === a.amount && o.term_months === a.term_months) return panelError('Условия не изменились — поменяйте сумму, срок или вид кредита.');
      path += '/decision';
      body = { action: 'counter_offer', reason: comment, product_id: o.product_id, amount: o.amount, term_months: o.term_months, rate: o.rate };
      done = 'Новые условия отправлены клиенту';
    } else if (kind === 'docs') {
      var docs = Array.prototype.slice.call($('review-foot').querySelectorAll('.ap-docs input:checked')).map(function (i) { return i.value; });
      if (!docs.length) return panelError('Отметьте хотя бы один документ.');
      path += '/decision';
      body = {
        action: 'request_info',
        info_request: 'Приложите: ' + docs.map(Catalog.documentTitle).join(', ') + (comment ? '. ' + comment : '') + '.',
      };
      done = 'Запрос документов отправлен клиенту';
    } else {
      // Заявка уже закреплена за сотрудником — «отложить» значит просто закрыть окно
      closeDialog();
      toast('Заявка отложена — она в списке «Отложенные»');
      loadList();
      return;
    }

    state.busy = true;
    button.disabled = true;
    var res = await api(path, { method: 'POST', json: body, idempotencyKey: api.newKey() });
    state.busy = false;
    button.disabled = false;
    if (!res.ok) {
      panelError(errorText(res));
      return;
    }
    closeDialog();
    toast(done);
    loadList();
  }

  // ---------- Старт ----------

  document.addEventListener('click', function (e) {
    var r = e.target.closest('.q-row');
    if (r) openApp(r.dataset.id);
  });

  $('btn-logout').addEventListener('click', function () {
    Auth.clear();
    window.location.replace('/employee/login/');
  });

  loadMe();
  loadList();
})();
