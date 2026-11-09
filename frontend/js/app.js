'use strict';
document.addEventListener('click', async (event) => {
  const target = event.target.closest('button');
  if (!target) return;
  if (target.dataset.open) document.getElementById(target.dataset.open)?.showModal();
  if (target.hasAttribute('data-close')) target.closest('dialog')?.close();
  if (target.dataset.copy) {
    try { await navigator.clipboard.writeText(target.dataset.copy); target.textContent = 'Copied'; }
    catch { target.textContent = 'Select and copy the key above'; }
  }
});
document.querySelectorAll('form[data-confirm]').forEach((form) => form.addEventListener('submit', (event) => {
  if (!window.confirm(form.dataset.confirm)) event.preventDefault();
}));
document.getElementById('navigation-toggle')?.addEventListener('click', (event) => {
  const navigation = document.getElementById('navigation');
  const expanded = event.currentTarget.getAttribute('aria-expanded') === 'true';
  event.currentTarget.setAttribute('aria-expanded', String(!expanded));
  navigation.classList.toggle('hidden', expanded);
  navigation.classList.toggle('flex', !expanded);
});
