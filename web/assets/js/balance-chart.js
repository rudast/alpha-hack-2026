/*
 * balance-chart.js — график остатка долга по кредиту: убывающая линия от выдачи до последнего платежа,
 * точка на каждой дате платежа, подсказка при наведении и с клавиатуры (стрелки влево/вправо).
 * Подключайте после ui.js.
 *
 *   BalanceChart.render(container, loan)
 *
 * loan — кредит из GET /loans: { amount, issued_at, schedule: [{ number, date, amount, principal,
 * interest, balance_after, status: PAID | NEXT | OVERDUE | PLANNED }] }.
 *
 * Прошлое — сплошная линия, будущее (план) — пунктир. Оплаченные платежи — закрашенные точки,
 * будущие — полые, ближайший или просроченный — крупная точка с ореолом. Цвет точки всегда
 * продублирован подписью статуса в подсказке.
 */
(function () {
  'use strict';

  var NS = 'http://www.w3.org/2000/svg';
  var fmt = UI.fmt;

  var PAD = { top: 28, right: 18, bottom: 34, left: 70 };
  var MIN_DOT_GAP = 11; // если точки стоят теснее, рисуем только ключевые — остальные видны при наведении

  var STATUS = {
    START: { label: 'Выдача кредита', cls: 'start' },
    PAID: { label: 'Оплачен', cls: 'paid' },
    NEXT: { label: 'Ближайший платёж', cls: 'next' },
    OVERDUE: { label: 'Просрочен', cls: 'overdue' },
    PLANNED: { label: 'Запланирован', cls: 'planned' },
  };

  var monthFmt = new Intl.DateTimeFormat('ru-RU', { month: 'short' });

  function el(name, attrs, parent) {
    var node = document.createElementNS(NS, name);
    Object.keys(attrs || {}).forEach(function (k) {
      node.setAttribute(k, attrs[k]);
    });
    if (parent) parent.appendChild(node);
    return node;
  }

  function html(tag, cls, text) {
    var node = document.createElement(tag);
    if (cls) node.className = cls;
    if (text != null) node.textContent = text;
    return node;
  }

  // «1,5 млн», «750 тыс.», «0»
  function axisMoney(v) {
    if (!v) return '0';
    if (v >= 1e6) return new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 1 }).format(v / 1e6) + ' млн';
    return Math.round(v / 1000) + ' тыс.';
  }

  // Красивый шаг шкалы: 1, 2, 2.5, 5 × 10^n
  function niceStep(max, count) {
    var raw = max / count;
    var pow = Math.pow(10, Math.floor(Math.log10(raw)));
    var steps = [1, 2, 2.5, 5, 10];
    for (var i = 0; i < steps.length; i++) if (steps[i] * pow >= raw) return steps[i] * pow;
    return 10 * pow;
  }

  // «мар 26»
  function monthLabel(d) {
    return monthFmt.format(d).replace('.', '') + ' ' + String(d.getFullYear()).slice(2);
  }

  function points(loan) {
    var start = UI.parseDate(loan.issued_at);
    var list = [{ date: start, balance: loan.amount, status: 'START', row: null }];
    loan.schedule.forEach(function (p) {
      list.push({ date: UI.parseDate(p.date), balance: p.balance_after, status: p.status, row: p });
    });
    return list;
  }

  function render(container, loan) {
    container.innerHTML = '';
    container.classList.add('bchart');

    var data = points(loan);
    var width = Math.max(container.clientWidth, 300);
    var height = width < 520 ? 230 : 280;
    var plotW = width - PAD.left - PAD.right;
    var plotH = height - PAD.top - PAD.bottom;

    var t0 = data[0].date.getTime();
    var t1 = data[data.length - 1].date.getTime();
    var step = niceStep(loan.amount, 4);
    var yMax = Math.ceil(loan.amount / step) * step;

    function x(d) {
      return PAD.left + ((d.getTime() - t0) / (t1 - t0)) * plotW;
    }
    function y(v) {
      return PAD.top + plotH - (v / yMax) * plotH;
    }

    var nextIndex = -1;
    data.forEach(function (p, i) {
      if (nextIndex === -1 && (p.status === 'NEXT' || p.status === 'OVERDUE')) nextIndex = i;
    });
    var paidCount = data.filter(function (p) { return p.status === 'PAID'; }).length;
    var lastPaid = nextIndex > 0 ? nextIndex - 1 : paidCount; // граница «факт / план»

    var svg = el('svg', {
      width: width,
      height: height,
      viewBox: '0 0 ' + width + ' ' + height,
      class: 'bchart-svg',
      role: 'img',
      tabindex: '0',
      'aria-label':
        'График остатка долга: от ' + fmt.rub(loan.amount) + ' ' + fmt.date(loan.issued_at) +
        ' до нуля ' + fmt.date(loan.schedule[loan.schedule.length - 1].date) +
        '. Стрелками влево и вправо можно перемещаться по платежам.',
    }, null);

    // Градиент заливки под линией
    var defs = el('defs', {}, svg);
    var grad = el('linearGradient', { id: 'bchart-fill-' + loan.id, x1: 0, y1: 0, x2: 0, y2: 1 }, defs);
    el('stop', { offset: '0%', 'stop-color': 'var(--chart-line)', 'stop-opacity': 0.18 }, grad);
    el('stop', { offset: '100%', 'stop-color': 'var(--chart-line)', 'stop-opacity': 0 }, grad);

    // Сетка и ось Y
    var grid = el('g', { class: 'bchart-grid' }, svg);
    for (var v = 0; v <= yMax + 1; v += step) {
      el('line', { x1: PAD.left, x2: width - PAD.right, y1: y(v), y2: y(v) }, grid);
      var label = el('text', { x: PAD.left - 10, y: y(v) + 4, 'text-anchor': 'end', class: 'bchart-axis' }, grid);
      label.textContent = axisMoney(v);
    }

    // Ось X: подписи месяцев, не теснее ~70 px
    var months = (t1 - t0) / (30.44 * 86400000);
    var every = Math.max(1, Math.ceil(months / Math.max(2, Math.floor(plotW / 70))));
    var first = new Date(data[0].date.getFullYear(), data[0].date.getMonth() + 1, 1);
    for (var m = 0, d = first; d.getTime() <= t1; m++, d = new Date(first.getFullYear(), first.getMonth() + m, 1)) {
      if (m % every) continue;
      var tx = el('text', { x: x(d), y: height - 10, 'text-anchor': 'middle', class: 'bchart-axis' }, svg);
      tx.textContent = monthLabel(d);
    }

    // Площадь и линия: факт сплошной, план пунктиром
    function path(from, to) {
      return data.slice(from, to + 1).map(function (p, i) {
        return (i ? 'L' : 'M') + x(p.date).toFixed(1) + ' ' + y(p.balance).toFixed(1);
      }).join(' ');
    }
    var all = path(0, data.length - 1);
    el('path', {
      d: all + ' L' + x(data[data.length - 1].date).toFixed(1) + ' ' + y(0) + ' L' + PAD.left + ' ' + y(0) + ' Z',
      fill: 'url(#bchart-fill-' + loan.id + ')',
    }, svg);
    el('path', { d: path(0, lastPaid), class: 'bchart-line' }, svg);
    if (lastPaid < data.length - 1) el('path', { d: path(lastPaid, data.length - 1), class: 'bchart-line bchart-line-plan' }, svg);

    // Отметка «сегодня»
    var now = Date.now();
    if (now > t0 && now < t1) {
      var tdx = x(new Date(now));
      el('line', { x1: tdx, x2: tdx, y1: PAD.top - 6, y2: y(0), class: 'bchart-today' }, svg);
      var tt = el('text', { x: tdx, y: PAD.top - 12, 'text-anchor': 'middle', class: 'bchart-today-label' }, svg);
      tt.textContent = 'Сегодня';
    }

    // Точки
    var gap = plotW / (data.length - 1);
    // Частые точки с толстой обводкой режут линию и выглядят как пунктир — делаем их мельче
    var dense = gap < 24;
    var dots = el('g', { class: dense ? 'bchart-dense' : '' }, svg);
    data.forEach(function (p, i) {
      var key = i === nextIndex || i === 0;
      if (!key && gap < MIN_DOT_GAP) return;
      var cls = STATUS[p.status].cls;
      if (i === nextIndex) el('circle', { cx: x(p.date), cy: y(p.balance), r: 11, class: 'bchart-halo bchart-halo-' + cls }, dots);
      el('circle', { cx: x(p.date), cy: y(p.balance), r: i === nextIndex ? 6 : dense ? 3 : 4, class: 'bchart-dot bchart-dot-' + cls }, dots);
    });

    // Слой наведения: перекрестие и выделенная точка
    var hover = el('g', { class: 'bchart-hover', visibility: 'hidden' }, svg);
    var cross = el('line', { y1: PAD.top, y2: y(0), class: 'bchart-cross' }, hover);
    var focusDot = el('circle', { r: 6, class: 'bchart-dot' }, hover);
    var hit = el('rect', { x: PAD.left - gap / 2, y: 0, width: plotW + gap, height: height, fill: 'transparent' }, svg);

    container.appendChild(svg);

    // Подсказка — HTML-блок поверх графика
    var tip = html('div', 'bchart-tip');
    tip.hidden = true;
    container.appendChild(tip);

    var current = -1;

    function show(i) {
      current = i;
      var p = data[i];
      var px = x(p.date);
      var py = y(p.balance);
      var st = STATUS[p.status];

      hover.setAttribute('visibility', 'visible');
      cross.setAttribute('x1', px);
      cross.setAttribute('x2', px);
      focusDot.setAttribute('cx', px);
      focusDot.setAttribute('cy', py);
      focusDot.setAttribute('class', 'bchart-dot bchart-dot-' + st.cls + ' bchart-dot-focus');

      tip.innerHTML = '';
      var head = html('div', 'bchart-tip-head');
      head.appendChild(html('span', 'bchart-tip-date', fmt.date(p.row ? p.row.date : loan.issued_at)));
      head.appendChild(html('span', 'bchart-tip-status bchart-tip-' + st.cls, st.label));
      tip.appendChild(head);

      if (!p.row) {
        tip.appendChild(html('div', 'bchart-tip-value', fmt.rub(loan.amount)));
        tip.appendChild(html('div', 'bchart-tip-caption', 'сумма кредита'));
      } else {
        tip.appendChild(html('div', 'bchart-tip-value', fmt.rub(p.row.amount)));
        tip.appendChild(html('div', 'bchart-tip-caption', 'платёж № ' + p.row.number));
        [
          ['Основной долг', fmt.rub(p.row.principal)],
          ['Проценты', fmt.rub(p.row.interest)],
          ['Остаток после', fmt.rub(p.row.balance_after)],
        ].forEach(function (r) {
          var line = html('div', 'bchart-tip-row');
          line.appendChild(html('span', '', r[0]));
          line.appendChild(html('strong', '', r[1]));
          tip.appendChild(line);
        });
      }
      tip.hidden = false;

      // Подсказка справа от точки, а у правого края — слева; по вертикали не вылезает за график
      var tw = tip.offsetWidth;
      var th = tip.offsetHeight;
      var left = px + 16 + tw > width ? px - 16 - tw : px + 16;
      var top = Math.min(Math.max(py - th / 2, 0), height - th);
      tip.style.left = Math.max(0, left) + 'px';
      tip.style.top = top + 'px';
    }

    function hide() {
      current = -1;
      hover.setAttribute('visibility', 'hidden');
      tip.hidden = true;
    }

    function nearest(clientX) {
      var rect = svg.getBoundingClientRect();
      var mx = ((clientX - rect.left) / rect.width) * width;
      var best = 0;
      for (var i = 1; i < data.length; i++) {
        if (Math.abs(x(data[i].date) - mx) < Math.abs(x(data[best].date) - mx)) best = i;
      }
      return best;
    }

    hit.addEventListener('pointermove', function (e) {
      var i = nearest(e.clientX);
      if (i !== current) show(i);
    });
    hit.addEventListener('pointerleave', hide);
    hit.addEventListener('pointerdown', function (e) {
      show(nearest(e.clientX)); // касание на телефоне
    });

    svg.addEventListener('focus', function () {
      show(nextIndex > 0 ? nextIndex : 0);
    });
    svg.addEventListener('blur', hide);
    svg.addEventListener('keydown', function (e) {
      if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
        e.preventDefault();
        var i = current < 0 ? 0 : current + (e.key === 'ArrowRight' ? 1 : -1);
        show(Math.max(0, Math.min(data.length - 1, i)));
      } else if (e.key === 'Home' || e.key === 'End') {
        e.preventDefault();
        show(e.key === 'Home' ? 0 : data.length - 1);
      }
    });
  }

  // Перерисовка при изменении ширины (поворот телефона, ресайз окна)
  function mount(container, loan) {
    render(container, loan);
    if (typeof ResizeObserver === 'function') {
      var lastWidth = container.clientWidth;
      var ro = new ResizeObserver(function () {
        if (!container.isConnected) {
          ro.disconnect();
          return;
        }
        if (Math.abs(container.clientWidth - lastWidth) > 4) {
          lastWidth = container.clientWidth;
          render(container, loan);
        }
      });
      ro.observe(container);
    }
  }

  window.BalanceChart = { render: mount };
})();
