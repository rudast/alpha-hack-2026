/*
 * datepicker.js — поле даты как в банковских приложениях: ввод с маской ДД.ММ.ГГГГ (точки ставятся сами)
 * и календарь по кнопке. В календаре можно листать месяцы, по клику на заголовок — выбрать месяц и год.
 * Подключайте после ui.js.
 *
 *   DatePicker.html({ id, value, max, attrs })  — разметка поля; value и max в формате 'YYYY-MM-DD',
 *                                                 max: 'today' — нельзя выбрать дату позже сегодняшней
 *   DatePicker.enhance(root)                     — оживить все поля внутри root (вызывать после вставки разметки)
 *   DatePicker.parse('03.10.2026')               — '2026-10-03' или null, если даты не существует
 *   DatePicker.format('2026-10-03')              — '03.10.2026'
 *
 * Выбор в календаре записывает дату в поле и вызывает на нём событие input — как будто её ввели руками.
 * Клавиатура в календаре: стрелки — по дням, PageUp/PageDown — по месяцам, Enter — выбрать, Esc — закрыть.
 */
(function () {
  'use strict';

  var icon = UI.icon;

  var MONTHS = ['Январь', 'Февраль', 'Март', 'Апрель', 'Май', 'Июнь', 'Июль', 'Август', 'Сентябрь', 'Октябрь', 'Ноябрь', 'Декабрь'];
  var MONTHS_SHORT = ['янв', 'фев', 'мар', 'апр', 'май', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек'];
  var WEEKDAYS = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс'];
  var fullFmt = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric', weekday: 'long' });

  function pad(n) {
    return (n < 10 ? '0' : '') + n;
  }

  function iso(d) {
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
  }

  function fromIso(value) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value || '')) return null;
    var p = value.split('-');
    return new Date(+p[0], +p[1] - 1, +p[2]);
  }

  function today() {
    var d = new Date();
    d.setHours(0, 0, 0, 0);
    return d;
  }

  function format(value) {
    var d = fromIso(value);
    return d ? pad(d.getDate()) + '.' + pad(d.getMonth() + 1) + '.' + d.getFullYear() : '';
  }

  // '31.02.2026' → null: такой даты нет
  function parse(text) {
    var m = /^(\d{2})\.(\d{2})\.(\d{4})$/.exec(text || '');
    if (!m) return null;
    var d = new Date(+m[3], +m[2] - 1, +m[1]);
    if (d.getDate() !== +m[1] || d.getMonth() !== +m[2] - 1 || +m[3] < 1900) return null;
    return iso(d);
  }

  // Маска: оставляем цифры и расставляем точки — '0310' → '03.10'
  function mask(text) {
    var digits = String(text).replace(/\D/g, '').slice(0, 8);
    var out = digits.slice(0, 2);
    if (digits.length > 2) out += '.' + digits.slice(2, 4);
    if (digits.length > 4) out += '.' + digits.slice(4);
    return out;
  }

  function html(opts) {
    var attrs = opts.attrs || '';
    return (
      '<div class="date-field"' + (opts.max ? ' data-max="' + opts.max + '"' : '') + '>' +
      '<div class="date-input">' +
      '<input type="text" inputmode="numeric" autocomplete="off" maxlength="10" placeholder="ДД.ММ.ГГГГ" id="' + opts.id + '" value="' +
      format(opts.value) + '" ' + attrs + ' />' +
      '<button type="button" class="date-btn" aria-label="Выбрать дату в календаре" aria-haspopup="dialog" aria-expanded="false">' +
      icon('calendar', 20) + '</button>' +
      '</div>' +
      '<div class="cal" role="dialog" aria-label="Календарь" hidden></div>' +
      '</div>'
    );
  }

  // ---------- Календарь ----------

  function Calendar(field) {
    this.field = field;
    this.input = field.querySelector('input');
    this.button = field.querySelector('.date-btn');
    this.box = field.querySelector('.cal');
    this.max = field.dataset.max === 'today' ? today() : fromIso(field.dataset.max);
    this.view = 'days'; // days | months | years
    this.cursor = today(); // день с фокусом и показываемый месяц
  }

  Calendar.prototype.selected = function () {
    return fromIso(parse(this.input.value));
  };

  Calendar.prototype.disabled = function (d) {
    return !!(this.max && d > this.max);
  };

  Calendar.prototype.open = function () {
    closeAll(this);
    var sel = this.selected();
    this.cursor = sel || (this.max && this.max < today() ? new Date(this.max) : today());
    this.view = 'days';
    this.box.hidden = false;
    this.button.setAttribute('aria-expanded', 'true');
    this.field.classList.add('is-open');
    this.render(true);
  };

  Calendar.prototype.close = function (focusButton) {
    if (this.box.hidden) return;
    this.box.hidden = true;
    this.button.setAttribute('aria-expanded', 'false');
    this.field.classList.remove('is-open');
    if (focusButton) this.button.focus();
  };

  Calendar.prototype.pick = function (d) {
    if (this.disabled(d)) return;
    this.input.value = pad(d.getDate()) + '.' + pad(d.getMonth() + 1) + '.' + d.getFullYear();
    this.input.dispatchEvent(new Event('input', { bubbles: true }));
    this.close(true);
  };

  Calendar.prototype.head = function (title, label) {
    return (
      '<div class="cal-head">' +
      '<button type="button" class="cal-nav" data-nav="-1" aria-label="Назад">' + icon('back', 18) + '</button>' +
      '<button type="button" class="cal-title" data-view-up aria-label="' + label + '">' + title + '</button>' +
      '<button type="button" class="cal-nav" data-nav="1" aria-label="Вперёд">' + icon('chevron', 18) + '</button>' +
      '</div>'
    );
  };

  Calendar.prototype.render = function (focus) {
    var c = this.cursor;
    var y = c.getFullYear();
    var m = c.getMonth();
    var sel = this.selected();
    var now = today();
    var out = '';

    if (this.view === 'days') {
      out += this.head(MONTHS[m] + ' ' + y, 'Выбрать месяц и год');
      out += '<div class="cal-weekdays">' + WEEKDAYS.map(function (w) { return '<span>' + w + '</span>'; }).join('') + '</div>';
      out += '<div class="cal-grid cal-days" role="grid">';
      var first = new Date(y, m, 1);
      var shift = (first.getDay() + 6) % 7; // неделя с понедельника
      var start = new Date(y, m, 1 - shift);
      for (var i = 0; i < 42; i++) {
        var d = new Date(start.getFullYear(), start.getMonth(), start.getDate() + i);
        var cls = ['cal-day'];
        if (d.getMonth() !== m) cls.push('is-other');
        if (+d === +now) cls.push('is-today');
        if (sel && +d === +sel) cls.push('is-selected');
        var off = this.disabled(d);
        out +=
          '<button type="button" class="' + cls.join(' ') + '" data-date="' + iso(d) + '"' +
          ' tabindex="' + (+d === +c ? '0' : '-1') + '"' + (off ? ' disabled' : '') +
          ' aria-label="' + fullFmt.format(d) + '"' + (sel && +d === +sel ? ' aria-selected="true"' : '') + '>' +
          d.getDate() + '</button>';
      }
      out += '</div>';
      var t = now;
      out +=
        '<div class="cal-foot">' +
        '<button type="button" class="cal-today" data-date="' + iso(t) + '"' + (this.disabled(t) ? ' disabled' : '') + '>Сегодня</button>' +
        '</div>';
    } else if (this.view === 'months') {
      out += this.head(String(y), 'Выбрать год');
      out += '<div class="cal-grid cal-months">';
      for (var mi = 0; mi < 12; mi++) {
        var md = new Date(y, mi, 1);
        var mcls = 'cal-cell' + (mi === m ? ' is-selected' : '') + (y === now.getFullYear() && mi === now.getMonth() ? ' is-today' : '');
        out += '<button type="button" class="' + mcls + '" data-month="' + mi + '"' + (this.disabled(md) ? ' disabled' : '') + '>' + MONTHS_SHORT[mi] + '</button>';
      }
      out += '</div>';
    } else {
      var from = y - (y % 12);
      out += this.head(from + ' — ' + (from + 11), 'Годы');
      out += '<div class="cal-grid cal-years">';
      for (var yi = from; yi < from + 12; yi++) {
        var ycls = 'cal-cell' + (yi === y ? ' is-selected' : '') + (yi === now.getFullYear() ? ' is-today' : '');
        out += '<button type="button" class="' + ycls + '" data-year="' + yi + '"' + (this.disabled(new Date(yi, 0, 1)) ? ' disabled' : '') + '>' + yi + '</button>';
      }
      out += '</div>';
    }

    this.box.innerHTML = out;
    if (this.view === 'years') this.box.querySelector('[data-view-up]').disabled = true;
    if (focus) {
      var target = this.box.querySelector('.cal-day[tabindex="0"]:not([disabled])') ||
        this.box.querySelector('.cal-cell.is-selected:not([disabled])') ||
        this.box.querySelector('.cal-title');
      if (target) target.focus();
    }
  };

  Calendar.prototype.move = function (days, months) {
    var c = this.cursor;
    var next = months
      ? new Date(c.getFullYear(), c.getMonth() + months, Math.min(c.getDate(), 28))
      : new Date(c.getFullYear(), c.getMonth(), c.getDate() + days);
    this.cursor = next;
    this.render(true);
  };

  Calendar.prototype.onClick = function (e) {
    var b = e.target.closest('button');
    if (!b || b.disabled) return;
    var c = this.cursor;
    if (b.dataset.date) {
      this.pick(fromIso(b.dataset.date));
    } else if (b.dataset.nav) {
      var dir = +b.dataset.nav;
      if (this.view === 'days') this.cursor = new Date(c.getFullYear(), c.getMonth() + dir, 1);
      else if (this.view === 'months') this.cursor = new Date(c.getFullYear() + dir, c.getMonth(), 1);
      else this.cursor = new Date(c.getFullYear() + dir * 12, c.getMonth(), 1);
      this.render(false);
    } else if (b.hasAttribute('data-view-up')) {
      this.view = this.view === 'days' ? 'months' : 'years';
      this.render(true);
    } else if (b.dataset.month) {
      this.cursor = new Date(c.getFullYear(), +b.dataset.month, 1);
      this.view = 'days';
      this.render(true);
    } else if (b.dataset.year) {
      this.cursor = new Date(+b.dataset.year, c.getMonth(), 1);
      this.view = 'months';
      this.render(true);
    }
  };

  Calendar.prototype.onKey = function (e) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation(); // не закрывать окно, внутри которого стоит поле
      this.close(true);
      return;
    }
    if (this.view !== 'days' || !e.target.classList.contains('cal-day')) return;
    var steps = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 };
    if (steps[e.key]) {
      e.preventDefault();
      this.cursor = fromIso(e.target.dataset.date);
      this.move(steps[e.key], 0);
    } else if (e.key === 'PageUp' || e.key === 'PageDown') {
      e.preventDefault();
      this.cursor = fromIso(e.target.dataset.date);
      this.move(0, e.key === 'PageUp' ? -1 : 1);
    }
  };

  // ---------- Подключение к полям ----------

  var open = [];

  function closeAll(except) {
    open.forEach(function (cal) {
      if (cal !== except && cal.field.isConnected) cal.close(false);
    });
    open = open.filter(function (cal) { return cal.field.isConnected; });
  }

  document.addEventListener('pointerdown', function (e) {
    open.forEach(function (cal) {
      if (cal.field.isConnected && !cal.field.contains(e.target)) cal.close(false);
    });
  });

  function enhance(root) {
    root.querySelectorAll('.date-field').forEach(function (field) {
      if (field.dataset.ready) return;
      field.dataset.ready = '1';
      var cal = new Calendar(field);
      open.push(cal);

      cal.input.addEventListener('input', function () {
        var masked = mask(cal.input.value);
        if (masked !== cal.input.value) cal.input.value = masked;
      });
      cal.button.addEventListener('click', function () {
        if (cal.box.hidden) cal.open();
        else cal.close(true);
      });
      cal.box.addEventListener('click', function (e) {
        cal.onClick(e);
      });
      field.addEventListener('keydown', function (e) {
        if (!cal.box.hidden) cal.onKey(e);
      });
    });
  }

  window.DatePicker = { html: html, enhance: enhance, parse: parse, format: format };
})();
