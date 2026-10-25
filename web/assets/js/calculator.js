/*
 * calculator.js — калькулятор кредитов и переключение между калькулятором и входом в кабинет.
 *
 * Правила страницы:
 *  1. Недоступные продукты не показываются вообще: список строится из Loans.getAvailable().
 *  2. Пока клиент не тронул калькулятор, он ничего не видит в списке, а перейти ко входу «с расчётом»
 *     не может: кнопка «Заполнить заявку» появляется только вместе с подобранными кредитами.
 *     Войти можно и без расчёта — через вкладку «Войти в кабинет (без расчёта)».
 *     Если клиент уже вошёл, «Заполнить заявку» ведёт сразу во вкладку «Оформить новый кредит».
 *  3. Выбранный кредит уходит в личный кабинет только через браузер (sessionStorage, см. LoanDraft
 *     в loans.js). Бэкенд и база данных в этом не участвуют.
 *  4. Любое новое действие в калькуляторе стирает ранее сохранённый расчёт: перенести можно только
 *     то, что клиент видит на экране в момент нажатия «Заполнить заявку».
 */
(function () {
  'use strict';

  var Loans = window.Loans;
  var Draft = window.LoanDraft;
  var fmt = Loans.fmt;

  var $ = function (id) {
    return document.getElementById(id);
  };

  var CABINET_URL = '/personalAccount/#new-loan'; // вкладка «Оформить новый кредит»

  // ---------- Состояние ----------

  var DEFAULTS = { org: 'ooo', collateral: 'without', age: 'gte9', amount: 1500000, term: 12 };
  var state = {
    org: DEFAULTS.org,
    collateral: DEFAULTS.collateral,
    age: DEFAULTS.age,
    amount: DEFAULTS.amount,
    term: DEFAULTS.term,
  };
  var touched = false; // клиент уже что-то менял в калькуляторе
  var expanded = {}; // какие карточки раскрыты («Узнать подробнее»), чтобы не терять это при пересчёте

  // ---------- Элементы ----------

  var form = $('calc-form');
  var results = $('calc-results');
  var amountInput = $('amount-input');
  var amountRange = $('amount-range');
  var termRange = $('term-range');
  var termOutput = $('term-output');
  var AMOUNT_STOPS = Loans.AMOUNT_STOPS;
  var TERM_STOPS = Loans.TERM_STOPS;

  // ---------- Иконки продуктов ----------

  var ICONS = {
    coins: '<ellipse cx="12" cy="7" rx="6" ry="3"/><path d="M6 7v5c0 1.7 2.7 3 6 3s6-1.3 6-3V7"/><path d="M6 12v5c0 1.7 2.7 3 6 3s6-1.3 6-3v-5"/>',
    briefcase: '<rect x="4" y="8" width="16" height="11" rx="2"/><path d="M9 8V6a2 2 0 012-2h2a2 2 0 012 2v2M4 13h16"/>',
    bag: '<path d="M6 8h12l1 12H5L6 8z"/><path d="M9 8V6a3 3 0 016 0v2"/>',
    rocket: '<path d="M12 3c3 2 5 5 5 9l-2 3H9l-2-3c0-4 2-7 5-9z"/><circle cx="12" cy="10" r="1.6"/><path d="M9 15l-2 4 3-1M15 15l2 4-3-1"/>',
    plus: '<circle cx="12" cy="12" r="8"/><path d="M9 12h6M12 9v6"/>',
    card: '<rect x="3" y="6" width="18" height="12" rx="2"/><path d="M3 10h18M7 15h4"/>',
    cycle: '<path d="M4 12a8 8 0 0113.5-5.8L20 8"/><path d="M20 4v4h-4"/><path d="M20 12a8 8 0 01-13.5 5.8L4 16"/><path d="M4 20v-4h4"/>',
    gear: '<circle cx="12" cy="12" r="3"/><path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M18.4 5.6l-2.1 2.1M7.7 16.3l-2.1 2.1"/>',
    car: '<path d="M5 15l1.6-5A2 2 0 018.5 8.6h7a2 2 0 011.9 1.4L19 15"/><rect x="3.5" y="15" width="17" height="4" rx="1.5"/><path d="M7 19v1.5M17 19v1.5"/>',
    house: '<path d="M4 11l8-7 8 7"/><path d="M6 10v10h12V10"/><path d="M10 20v-5h4v5"/>',
    factory: '<path d="M3 20h18"/><path d="M5 20V11l5 3v-3l5 3V5h4v15"/>',
    sliders: '<path d="M4 7h9M17 7h3M4 17h3M11 17h9"/><circle cx="15" cy="7" r="2"/><circle cx="9" cy="17" r="2"/>',
  };

  function icon(name, size) {
    return (
      '<svg width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" ' +
      'stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
      (ICONS[name] || '') + '</svg>'
    );
  }

  // ---------- Ползунки ----------

  function nearestIndex(stops, value) {
    var best = 0;
    var bestDiff = Infinity;
    for (var i = 0; i < stops.length; i++) {
      var diff = Math.abs(stops[i] - value);
      if (diff < bestDiff) {
        best = i;
        bestDiff = diff;
      }
    }
    return best;
  }

  function setFill(range) {
    var max = Number(range.max);
    range.style.setProperty('--p', max > 0 ? String(Number(range.value) / max) : '0');
  }

  function buildScale(container, stops, marks) {
    container.innerHTML = '';
    marks.forEach(function (mark, i) {
      var span = document.createElement('span');
      span.textContent = mark.label;
      span.style.left = (nearestIndex(stops, mark.value) / (stops.length - 1)) * 100 + '%';
      if (i === 0) span.className = 'is-first';
      if (i === marks.length - 1) span.className = 'is-last';
      container.appendChild(span);
    });
  }

  amountRange.max = String(AMOUNT_STOPS.length - 1);
  termRange.max = String(TERM_STOPS.length - 1);

  buildScale(
    $('amount-scale'),
    AMOUNT_STOPS,
    [
      { value: 50000, label: '50 тыс.' },
      { value: 1000000, label: '1 млн' },
      { value: 5000000, label: '5 млн' },
      { value: 30000000, label: '30 млн ₽' },
    ]
  );
  buildScale(
    $('term-scale'),
    TERM_STOPS,
    [
      { value: 1, label: '1 мес.' },
      { value: 12, label: '1 год' },
      { value: 60, label: '5 лет' },
      { value: 120, label: '10 лет' },
    ]
  );

  // Ширина поля = ширина текста, чтобы «₽» стоял вплотную к числу
  var measure = document.createElement('canvas').getContext('2d');
  function fitAmountInput() {
    var cs = window.getComputedStyle(amountInput);
    measure.font = cs.fontWeight + ' ' + cs.fontSize + ' ' + cs.fontFamily;
    amountInput.style.width = Math.ceil(measure.measureText(amountInput.value || '0').width) + 2 + 'px';
  }

  function syncAmount() {
    amountInput.value = fmt.number(state.amount);
    fitAmountInput();
    amountRange.value = String(nearestIndex(AMOUNT_STOPS, state.amount));
    amountRange.setAttribute('aria-valuetext', fmt.rub(state.amount));
    setFill(amountRange);
  }

  function syncTerm() {
    termOutput.textContent = fmt.term(state.term);
    termRange.value = String(nearestIndex(TERM_STOPS, state.term));
    termRange.setAttribute('aria-valuetext', fmt.term(state.term));
    setFill(termRange);
  }

  function syncControls() {
    ['org', 'collateral', 'age'].forEach(function (name) {
      var el = form.querySelector('input[name="' + name + '"][value="' + state[name] + '"]');
      if (el) el.checked = true;
    });
    syncAmount();
    syncTerm();
  }

  // Сумму можно ввести руками: применяем при уходе из поля или по Enter
  function commitAmount() {
    var previous = state.amount;
    var digits = amountInput.value.replace(/\D/g, '');
    var value = digits ? parseInt(digits, 10) : state.amount;
    value = Math.round(value / 1000) * 1000;
    state.amount = Math.min(Loans.LIMITS.amountMax, Math.max(Loans.LIMITS.amountMin, value));
    syncAmount();
    // Если сумма не изменилась, список не перерисовываем: иначе уход фокуса из поля
    // заменит кнопки под курсором, и клик по ним потеряется.
    if (state.amount !== previous) render();
  }

  // ---------- Взаимодействие клиента с калькулятором ----------

  function markTouched() {
    var first = !touched;
    touched = true;
    if (Draft.get()) {
      // параметры меняются — ранее сохранённый расчёт больше не соответствует экрану
      Draft.clear();
      refreshNav();
    }
    if (first) render();
  }

  form.addEventListener('submit', function (e) {
    e.preventDefault(); // Enter в поле суммы не должен перезагружать страницу
  });

  ['input', 'change', 'click'].forEach(function (type) {
    form.addEventListener(type, markTouched);
  });

  form.addEventListener('change', function (e) {
    var name = e.target.name;
    if (name === 'org' || name === 'collateral' || name === 'age') {
      state[name] = e.target.value;
      render();
    }
  });

  amountRange.addEventListener('input', function () {
    markTouched();
    state.amount = AMOUNT_STOPS[Number(amountRange.value)];
    syncAmount();
    render();
  });

  termRange.addEventListener('input', function () {
    markTouched();
    state.term = TERM_STOPS[Number(termRange.value)];
    syncTerm();
    render();
  });

  amountInput.addEventListener('input', function () {
    amountInput.value = amountInput.value.replace(/[^\d\s]/g, '');
    fitAmountInput();
  });
  amountInput.addEventListener('focus', function () {
    amountInput.select();
  });
  amountInput.addEventListener('blur', commitAmount);
  amountInput.addEventListener('keydown', function (e) {
    if (e.key === 'Enter') {
      e.preventDefault();
      commitAmount();
    }
  });

  // ---------- Список подходящих кредитов ----------

  function query() {
    return { org: state.org, collateral: state.collateral, age: state.age, amount: state.amount, term: state.term };
  }

  function metric(label, value) {
    return '<div><dt>' + label + '</dt><dd>' + value + '</dd></div>';
  }

  function detailsHtml(p) {
    var collateralText = { without: 'Не требуется', with: 'Требуется', any: 'Не важен: подходит и с залогом, и без' };
    var orgText = p.org.length === 2 ? 'ООО и ИП' : p.org[0] === 'ooo' ? 'Только ООО' : 'Только ИП';
    var amountText = fmt.amountRange(p.amount[0], p.amount[1]);
    if (p.youngMax) {
      amountText += '. Для бизнеса младше 9 месяцев — до ' + fmt.amountShort(p.youngMax) + '\u00a0₽';
    }
    var facts = [
      ['Для кого', orgText],
      ['Залог', collateralText[p.collateral]],
      ['Срок ведения бизнеса', p.needs9 ? 'От 9 месяцев' : 'Любой, в том числе меньше 9 месяцев'],
      ['Сумма', amountText],
      ['Срок', fmt.termRange(p.term[0], p.term[1])],
    ];
    return (
      '<dl class="loan-facts">' +
      facts.map(function (f) { return '<dt>' + f[0] + '</dt><dd>' + f[1] + '</dd>'; }).join('') +
      '</dl>' +
      '<p class="loan-disclaimer">Ставка и платёж — предварительный расчёт. Итоговые условия банк определит при рассмотрении заявки.</p>'
    );
  }

  function cardHtml(item, index, all) {
    var p = item.product;
    var open = !!expanded[p.id];
    var best = index === 0 && all.length > 1 ? '<span class="tag tag-best">Самая низкая ставка</span>' : '';
    return (
      '<article class="loan-card">' +
        '<div class="loan-card-top">' +
          '<div>' +
            '<div class="loan-tags"><span class="tag">' + Loans.CATEGORIES[p.category] + '</span>' + best + '</div>' +
            '<h2 class="loan-title">' + p.title + '</h2>' +
            '<p class="loan-desc">' + p.desc + '</p>' +
          '</div>' +
          '<div class="loan-icon">' + icon(p.icon, 28) + '</div>' +
        '</div>' +
        '<dl class="loan-metrics">' +
          metric('Ставка', fmt.rate(p.rate)) +
          metric('Переплата', fmt.rub(item.overpayment)) +
          metric('Платёж в месяц', fmt.rub(item.payment)) +
        '</dl>' +
        '<div class="loan-actions">' +
          '<button type="button" class="btn btn-secondary btn-md" data-action="details" data-id="' + p.id + '"' +
            ' aria-expanded="' + open + '" aria-controls="details-' + p.id + '">' +
            (open ? 'Свернуть условия' : 'Узнать подробнее') + '</button>' +
          '<button type="button" class="btn btn-primary btn-md" data-action="apply" data-id="' + p.id + '">Заполнить заявку</button>' +
        '</div>' +
        '<div class="loan-details' + (open ? '' : ' hidden') + '" id="details-' + p.id + '">' + detailsHtml(p) + '</div>' +
      '</article>'
    );
  }

  function emptyHtml(iconName, title, text, withReset) {
    return (
      '<div class="calc-empty">' +
        '<div class="calc-empty-icon">' + icon(iconName, 28) + '</div>' +
        '<h2>' + title + '</h2>' +
        '<p>' + text + '</p>' +
        (withReset ? '<button type="button" class="btn btn-secondary btn-md" data-action="reset">Сбросить параметры</button>' : '') +
      '</div>'
    );
  }

  function render() {
    if (!touched) {
      results.innerHTML = emptyHtml(
        'sliders',
        'Задайте параметры кредита',
        'Выберите тип регистрации, залог, сумму и срок — покажем только те кредиты, которые вам доступны, и сразу посчитаем платёж.',
        false
      );
      return;
    }

    var list = Loans.getAvailable(query());

    if (!list.length) {
      results.innerHTML = emptyHtml(
        'sliders',
        'Под эти параметры кредитов нет',
        'Попробуйте изменить сумму, срок или наличие залога — список обновится сразу.',
        true
      );
      return;
    }

    results.innerHTML = list.map(cardHtml).join('');
  }

  results.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-action]');
    if (!btn) return;
    var action = btn.getAttribute('data-action');
    var id = btn.getAttribute('data-id');

    if (action === 'details') {
      // Раскрываем на месте, без перерисовки списка, чтобы не терять фокус
      var panel = $('details-' + id);
      var open = panel.classList.contains('hidden');
      panel.classList.toggle('hidden', !open);
      btn.setAttribute('aria-expanded', String(open));
      btn.textContent = open ? 'Свернуть условия' : 'Узнать подробнее';
      expanded[id] = open;
    } else if (action === 'apply') {
      applyFor(id);
    } else if (action === 'reset') {
      resetParams();
    }
  });

  function resetParams() {
    state.org = DEFAULTS.org;
    state.collateral = DEFAULTS.collateral;
    state.age = DEFAULTS.age;
    state.amount = DEFAULTS.amount;
    state.term = DEFAULTS.term;
    syncControls();
    render();
  }

  // «Заполнить заявку»: сохраняем расчёт в браузере и идём дальше — во вход или сразу в кабинет
  function applyFor(productId) {
    var q = query();
    var product = Loans.productById(productId);
    if (!product || !Loans.isAvailable(product, q)) return; // кредит мог перестать подходить

    Draft.save({
      productId: productId,
      org: q.org,
      collateral: q.collateral,
      age: q.age,
      amount: q.amount,
      term: q.term,
    });
    refreshNav();

    if (window.Auth && Auth.token()) {
      window.location.href = CABINET_URL; // клиент уже вошёл
      return;
    }
    go('auth');
  }

  // ---------- Вкладки: калькулятор / вход ----------

  var lastView = '';

  function currentView() {
    return window.location.hash === '#login' ? 'auth' : 'calc';
  }

  // Подпись вкладки честно говорит, едет ли расчёт в кабинет
  function refreshNav() {
    $('nav-auth').textContent = Draft.get() ? 'Вход в кабинет с расчётом' : 'Войти в кабинет (без расчёта)';
  }

  // Блок про перенос данных на форме входа
  function refreshAuthDraft() {
    var draft = Draft.get();
    var info = draft ? Loans.fromDraft(draft) : null;
    var skip = $('check-skip-carry');
    var summary = $('draft-summary');

    if (info) {
      $('draft-summary-title').textContent = info.product.title;
      $('draft-summary-meta').textContent =
        fmt.rub(info.query.amount) + ' на ' + fmt.term(info.query.term) + '. ' + Loans.queryText(info.query) + '.';
      summary.classList.remove('hidden');
      if (skip.disabled) {
        skip.disabled = false;
        skip.checked = false; // по умолчанию расчёт едет в кабинет
      }
    } else {
      summary.classList.add('hidden');
      skip.checked = true;
      skip.disabled = true; // переносить нечего
    }
    summary.classList.toggle('is-skipped', skip.checked && !skip.disabled);
  }

  function renderView() {
    var view = currentView();
    $('view-calc').classList.toggle('hidden', view !== 'calc');
    $('view-auth').classList.toggle('hidden', view !== 'auth');

    var navCalc = $('nav-calc');
    var navAuth = $('nav-auth');
    if (view === 'calc') {
      navCalc.setAttribute('aria-current', 'page');
      navAuth.removeAttribute('aria-current');
    } else {
      navAuth.setAttribute('aria-current', 'page');
      navCalc.removeAttribute('aria-current');
    }

    document.title = (view === 'calc' ? 'Калькулятор кредитов' : 'Вход в кабинет') + ' — Альфа-Будущее';
    refreshNav();
    if (view === 'auth') refreshAuthDraft();
    if (lastView && lastView !== view) window.scrollTo(0, 0);
    lastView = view;
  }

  function go(view) {
    var hash = view === 'auth' ? '#login' : '#calc';
    if (window.location.hash === hash) {
      renderView();
    } else {
      window.location.hash = hash; // дальше сработает hashchange
    }
  }

  window.addEventListener('hashchange', renderView);

  $('btn-to-calc').addEventListener('click', function () {
    go('calc');
  });

  // Явный выбор «войти без расчёта»: переносить нечего
  $('btn-skip-calc').addEventListener('click', function () {
    Draft.clear();
    go('auth');
  });

  $('check-skip-carry').addEventListener('change', function () {
    $('draft-summary').classList.toggle('is-skipped', this.checked);
  });

  // ---------- Старт ----------

  syncControls();
  render();
  renderView();
})();
