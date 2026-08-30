import type { Attrs } from './element.js'

export interface ActionOptions {
  confirm?: string
  busy?: string
  then?: 'reload' | 'none' | `redirect:${string}`
}

export interface LoadOptions {
  into: string
  pick?: string
  open?: string
  empty?: string
}

export interface LiveOptions {
  pick?: string
  every?: number
  empty?: string
}

export function act(method: string, url: string, options: ActionOptions = {}): Attrs {
  return {
    'data-act': `${method.toUpperCase()} ${url}`,
    'data-confirm': options.confirm,
    'data-busy': options.busy,
    'data-then': options.then,
  }
}

export function submits(method: string, url: string, options: ActionOptions = {}): Attrs {
  return { ...act(method, url, options), 'data-form': '' }
}

export function openDialog(id: string): Attrs {
  return { 'data-open': id }
}

export function closeDialog(id: string): Attrs {
  return { 'data-close': id }
}

export function loads(url: string, options: LoadOptions): Attrs {
  return {
    'data-load': url,
    'data-into': options.into,
    'data-pick': options.pick,
    'data-open': options.open,
    'data-empty': options.empty,
  }
}

export function live(url: string, options: LiveOptions = {}): Attrs {
  return {
    'data-live': url,
    'data-pick': options.pick,
    'data-every': options.every,
    'data-empty': options.empty,
  }
}

export function field(path: string): Attrs {
  return { 'data-field': path }
}

export function copies(value: string): Attrs {
  return { 'data-copy': value }
}

export function fills(selector: string, value: string): Attrs {
  return { 'data-fill': selector, 'data-fill-value': value }
}

export function tab(group: string, value: string, active = false): Attrs {
  return { type: 'button', 'data-tab': `${group}:${value}`, class: `shelf-btn shelf-btn-sm${active ? ' active' : ''}` }
}

export function panel(group: string, value: string, active = false): Attrs {
  return { 'data-panel': `${group}:${value}`, hidden: !active }
}

export function tabValue(group: string): Attrs {
  return { 'data-tab-value': group }
}

export function revealsWhen(selector: string, value: string): Attrs {
  return { 'data-reveal': selector, 'data-reveal-when': value }
}

export function revealed(id: string): Attrs {
  return { id, hidden: true }
}

export function matches(selector: string, message: string): Attrs {
  return { 'data-match': selector, 'data-match-message': message }
}

export function toggles(selectors: string, remember?: string): Attrs {
  return { 'data-toggle': selectors, 'data-remember': remember }
}

export function reloads(frameSelector: string): Attrs {
  return { 'data-reload': frameSelector }
}

export const RUNTIME_SCRIPT = `
(() => {
  const attr = (el, name) => el.getAttribute(name);

  const toast = (message, isError) => {
    let box = document.getElementById('shelf-toast');
    if (!box) {
      box = document.createElement('div');
      box.id = 'shelf-toast';
      box.style.cssText = 'position:fixed;bottom:24px;right:24px;padding:12px 20px;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius);box-shadow:var(--shadow);font-size:13px;z-index:300;max-width:420px';
      document.body.appendChild(box);
    }
    box.textContent = message;
    box.style.borderColor = isError ? 'var(--danger)' : 'var(--success)';
    box.style.display = 'block';
    clearTimeout(box.timer);
    box.timer = setTimeout(() => { box.style.display = 'none'; }, isError ? 8000 : 4000);
  };

  const request = async (method, url, body) => {
    const response = await fetch(url, {
      method,
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      body: body ? JSON.stringify(body) : undefined,
    });
    return response.json().catch(() => ({ ok: response.ok }));
  };

  const fieldValue = (input) => {
    if (input.type === 'checkbox') return input.checked;
    if (input.type === 'number') return input.value === '' ? null : Number(input.value);
    return input.value;
  };

  const formBody = (form) => {
    const body = {};
    for (const input of form.querySelectorAll('[name]')) {
      if (input.disabled) continue;
      const value = fieldValue(input);
      if (input.dataset.omitEmpty !== undefined && (value === '' || value === null)) continue;
      body[input.name] = value;
    }
    return body;
  };

  const mismatch = (form) => {
    for (const input of form.querySelectorAll('[data-match]')) {
      const other = form.querySelector(attr(input, 'data-match'));
      if (other && other.value !== input.value) return attr(input, 'data-match-message');
    }
    return null;
  };

  const pick = (result, path) => {
    const value = path ? path.split('.').reduce((acc, key) => (acc == null ? acc : acc[key]), result.data) : result.data;
    return Array.isArray(value) ? value.join('\\n') : value;
  };

  const fill = (target, text, fallback) => {
    target.textContent = text || fallback || '';
    target.scrollTop = target.scrollHeight;
  };

  const finish = (el, result) => {
    const then = attr(el, 'data-then') || 'reload';
    if (then === 'none') return;
    if (then.startsWith('redirect:')) {
      location.href = then.slice('redirect:'.length).replace('{id}', result?.data?.id ?? '');
      return;
    }
    location.reload();
  };

  const run = async (el, body) => {
    const [method, ...rest] = (attr(el, 'data-act') || '').split(' ');
    const url = rest.join(' ');
    if (!url) return;

    const confirmText = attr(el, 'data-confirm');
    if (confirmText && !confirm(confirmText)) return;

    const busyLabel = attr(el, 'data-busy');
    const original = el.textContent;
    const controls = el.form ? [...el.form.querySelectorAll('button')] : [el];
    controls.forEach((c) => { c.disabled = true; });
    if (busyLabel) el.textContent = busyLabel;

    try {
      const result = await request(method, url, body);
      if (result.ok) finish(el, result);
      else toast(result.error?.message || 'Request failed', true);
    } catch (err) {
      toast(err.message || 'Network error', true);
    } finally {
      controls.forEach((c) => { c.disabled = false; });
      if (busyLabel) el.textContent = original;
    }
  };

  const load = async (el) => {
    const target = document.querySelector(attr(el, 'data-into'));
    if (!target) return;
    try {
      const result = await request('GET', attr(el, 'data-load'));
      fill(target, pick(result, attr(el, 'data-pick')), attr(el, 'data-empty'));
    } catch (err) {
      fill(target, '', err.message);
    }
  };

  const refresh = async (el) => {
    try {
      const result = await request('GET', attr(el, 'data-live'));
      const fields = el.querySelectorAll('[data-field]');
      if (!fields.length) {
        fill(el, pick(result, attr(el, 'data-pick')), attr(el, 'data-empty'));
        return;
      }
      fields.forEach((target) => {
        const value = pick(result, attr(target, 'data-field'));
        target.textContent = value === undefined || value === null ? (attr(el, 'data-empty') || '—') : value;
      });
    } catch (err) {
      fill(el, '', err.message);
    }
  };

  const select = (el) => {
    const [group, value] = attr(el, 'data-tab').split(':');
    document.querySelectorAll('[data-panel^="' + group + ':"]').forEach((p) => {
      p.hidden = attr(p, 'data-panel') !== group + ':' + value;
    });
    document.querySelectorAll('[data-tab^="' + group + ':"]').forEach((b) => {
      b.classList.toggle('active', b === el);
    });
    document.querySelectorAll('[data-tab-value="' + group + '"]').forEach((input) => { input.value = value; });
  };

  const dialog = (id, visible) => {
    const el = id && document.getElementById(id);
    if (el) el.style.display = visible ? 'flex' : 'none';
  };

  const flip = (el) => {
    const targets = [...document.querySelectorAll(attr(el, 'data-toggle'))];
    targets.forEach((target) => { target.hidden = !target.hidden; });
    const key = attr(el, 'data-remember');
    if (key) localStorage.setItem(key, targets.map((t) => (t.hidden ? '1' : '0')).join(''));
  };

  const restore = (el) => {
    const key = attr(el, 'data-remember');
    const stored = key && localStorage.getItem(key);
    if (!stored) return;
    [...document.querySelectorAll(attr(el, 'data-toggle'))].forEach((target, index) => {
      target.hidden = stored[index] === '1';
    });
  };

  const HANDLED = '[data-act],[data-load],[data-copy],[data-fill],[data-tab],[data-toggle],[data-reload],[data-open],[data-close]';

  document.addEventListener('click', (event) => {
    const el = event.target.closest(HANDLED);
    if (!el) return;

    if (el.hasAttribute('data-copy')) {
      navigator.clipboard.writeText(attr(el, 'data-copy')).then(() => toast('Copied'));
      return;
    }
    if (el.hasAttribute('data-fill')) {
      const target = document.querySelector(attr(el, 'data-fill'));
      if (target) target.value = attr(el, 'data-fill-value');
    }
    if (el.hasAttribute('data-tab')) { select(el); return; }
    if (el.hasAttribute('data-toggle')) { flip(el); return; }
    if (el.hasAttribute('data-reload')) {
      const frame = document.querySelector(attr(el, 'data-reload'));
      if (frame) frame.contentWindow.location.reload();
      return;
    }
    if (el.hasAttribute('data-load')) { event.preventDefault(); load(el); }
    else if (el.hasAttribute('data-act') && !el.hasAttribute('data-form')) { event.preventDefault(); run(el, null); return; }

    dialog(attr(el, 'data-open'), true);
    dialog(attr(el, 'data-close'), false);
  });

  document.addEventListener('submit', (event) => {
    const form = event.target.closest('form[data-act]');
    if (!form) return;
    event.preventDefault();
    const conflict = mismatch(form);
    if (conflict) return toast(conflict, true);
    const submitter = event.submitter || form.querySelector('button[type=submit]') || form;
    run({
      getAttribute: (name) => form.getAttribute(name),
      hasAttribute: (name) => form.hasAttribute(name),
      form,
      get textContent() { return submitter.textContent; },
      set textContent(value) { submitter.textContent = value; },
    }, formBody(form));
  });

  document.addEventListener('change', (event) => {
    const source = event.target;
    if (!source.hasAttribute || !source.hasAttribute('data-reveal')) return;
    const target = document.querySelector(attr(source, 'data-reveal'));
    if (target) target.hidden = source.value !== attr(source, 'data-reveal-when');
  });

  document.addEventListener('DOMContentLoaded', () => {
    document.querySelectorAll('[data-remember]').forEach(restore);
    document.querySelectorAll('[data-live]').forEach((el) => {
      refresh(el);
      const every = Number(attr(el, 'data-every'));
      if (every) setInterval(() => refresh(el), every);
    });
  });
})();`
