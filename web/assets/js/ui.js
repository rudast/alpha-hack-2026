/*
 * ui.js — мелкие помощники для страниц кабинета: иконки, экранирование, форматирование дат и денег.
 * Подключайте после loans.js (берёт оттуда форматирование сумм и сроков).
 */
(function () {
  'use strict';

  // Те же линейные иконки, что в калькуляторе
  var ICONS = {
    coins: '<ellipse cx="12" cy="7" rx="6" ry="3"/><path d="M6 7v5c0 1.7 2.7 3 6 3s6-1.3 6-3V7"/><path d="M6 12v5c0 1.7 2.7 3 6 3s6-1.3 6-3v-5"/>',
    bag: '<path d="M6 8h12l1 12H5L6 8z"/><path d="M9 8V6a3 3 0 016 0v2"/>',
    cycle: '<path d="M4 12a8 8 0 0113.5-5.8L20 8"/><path d="M20 4v4h-4"/><path d="M20 12a8 8 0 01-13.5 5.8L4 16"/><path d="M4 20v-4h4"/>',
    gear: '<circle cx="12" cy="12" r="3"/><path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M18.4 5.6l-2.1 2.1M7.7 16.3l-2.1 2.1"/>',
    car: '<path d="M5 15l1.6-5A2 2 0 018.5 8.6h7a2 2 0 011.9 1.4L19 15"/><rect x="3.5" y="15" width="17" height="4" rx="1.5"/><path d="M7 19v1.5M17 19v1.5"/>',
    house: '<path d="M4 11l8-7 8 7"/><path d="M6 10v10h12V10"/><path d="M10 20v-5h4v5"/>',
    factory: '<path d="M3 20h18"/><path d="M5 20V11l5 3v-3l5 3V5h4v15"/>',
    check: '<path d="M5 12.5l4.5 4.5L19 7.5"/>',
    close: '<path d="M6 6l12 12M18 6L6 18"/>',
    back: '<path d="M15 5l-7 7 7 7"/>',
    chevron: '<path d="M9 5l7 7-7 7"/>',
    file: '<path d="M7 3h7l5 5v13H7z"/><path d="M14 3v5h5"/>',
    clip: '<path d="M20 11.5l-8 8a5 5 0 01-7-7l8.5-8.5a3.3 3.3 0 014.7 4.7l-8.4 8.4a1.7 1.7 0 01-2.4-2.4l7.6-7.6"/>',
    alert: '<path d="M12 4l9 16H3z"/><path d="M12 10v4M12 17.5v.01"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8v.01"/>',
    spark: '<path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/><path d="M19 16l.8 2.2L22 19l-2.2.8L19 22l-.8-2.2L16 19l2.2-.8z"/>',
    building: '<rect x="5" y="3" width="14" height="18" rx="1.5"/><path d="M9 7h2M13 7h2M9 11h2M13 11h2M9 15h2M13 15h2M10 21v-3h4v3"/>',
    plus: '<path d="M12 5v14M5 12h14"/>',
  };

  function icon(name, size) {
    size = size || 20;
    return (
      '<svg width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" ' +
      'stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
      (ICONS[name] || '') + '</svg>'
    );
  }

  // Всё, что пришло с сервера или из моков, вставляем в разметку только через esc()
  function esc(value) {
    return String(value == null ? '' : value)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  var dateFmt = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' });
  var dateShortFmt = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long' });
  var dateTimeFmt = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' });
  var ratioFmt = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 1, maximumFractionDigits: 2 });

  // '2026-10-24' разбираем как местную дату, а не UTC, иначе в западных поясах уедет на день
  function parseDate(value) {
    if (!value) return null;
    if (/^\d{4}-\d{2}-\d{2}$/.test(value)) {
      var p = value.split('-');
      return new Date(+p[0], +p[1] - 1, +p[2]);
    }
    var d = new Date(value);
    return isNaN(d) ? null : d;
  }

  function today() {
    var d = new Date();
    d.setHours(0, 0, 0, 0);
    return d;
  }

  // Сколько дней от сегодня до даты: отрицательное — дата прошла
  function daysUntil(value) {
    var d = parseDate(value);
    if (!d) return null;
    d.setHours(0, 0, 0, 0);
    return Math.round((d - today()) / 86400000);
  }

  // Число месяцев между датой и сегодня (полных)
  function monthsSince(value) {
    var d = parseDate(value);
    if (!d) return null;
    var t = today();
    var months = (t.getFullYear() - d.getFullYear()) * 12 + (t.getMonth() - d.getMonth());
    if (t.getDate() < d.getDate()) months--;
    return months;
  }

  var fmt = {
    rub: function (n) {
      return n == null ? '—' : window.Loans.fmt.rub(Math.round(n));
    },
    short: function (n) {
      return window.Loans.fmt.amountShort(n) + ' ₽';
    },
    term: function (m) {
      return window.Loans.fmt.term(m);
    },
    rate: function (r) {
      return window.Loans.fmt.rate(r);
    },
    ratio: function (r) {
      return r == null ? '—' : ratioFmt.format(r);
    },
    date: function (v) {
      var d = parseDate(v);
      return d ? dateFmt.format(d).replace(' г.', '') : '—';
    },
    dateShort: function (v) {
      var d = parseDate(v);
      return d ? dateShortFmt.format(d) : '—';
    },
    dateTime: function (v) {
      var d = parseDate(v);
      return d ? dateTimeFmt.format(d) : '—';
    },
    // «через 3 дня», «завтра», «сегодня», «5 дней назад»
    relDays: function (v) {
      var n = daysUntil(v);
      if (n == null) return '';
      if (n === 0) return 'сегодня';
      if (n === 1) return 'завтра';
      var abs = Math.abs(n);
      var word = plural(abs, 'день', 'дня', 'дней');
      return n > 0 ? 'через ' + abs + ' ' + word : abs + ' ' + word + ' назад';
    },
    // Возраст бизнеса: «10 лет», «2 года 1 мес.»
    age: function (v) {
      var m = monthsSince(v);
      return m == null ? '—' : window.Loans.fmt.term(Math.max(m, 1));
    },
  };

  function plural(n, one, few, many) {
    var mod10 = n % 10;
    var mod100 = n % 100;
    if (mod10 === 1 && mod100 !== 11) return one;
    if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return few;
    return many;
  }

  // Оценка долговой нагрузки (долг / капитал): больше 2 ухудшает скоринг
  function debtLevel(ratio) {
    if (ratio == null) return { tone: 'danger', label: 'Не определена', text: 'Собственный капитал нулевой или отрицательный — банк оценит это как высокий риск.' };
    if (ratio <= 1) return { tone: 'success', label: 'Низкая', text: 'Долг меньше собственного капитала. Это хороший сигнал для банка.' };
    if (ratio <= 2) return { tone: 'warning', label: 'Умеренная', text: 'Долг больше капитала, но в пределах нормы. Новый кредит может её превысить.' };
    return { tone: 'danger', label: 'Высокая', text: 'Долг больше капитала в ' + ratioFmt.format(ratio) + ' раза. Это может снизить шансы на одобрение нового кредита.' };
  }

  window.UI = {
    icon: icon,
    esc: esc,
    fmt: fmt,
    plural: plural,
    parseDate: parseDate,
    daysUntil: daysUntil,
    monthsSince: monthsSince,
    debtLevel: debtLevel,
  };
})();
