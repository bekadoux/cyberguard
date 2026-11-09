(() => {
  let preference = 'system';
  try {
    const stored = localStorage.getItem('cyberguard.theme');
    if (stored === 'light' || stored === 'dark') preference = stored;
  } catch {
    // Browser privacy settings can disable storage. System preference still works.
  }
  const dark =
    preference === 'dark' ||
    (preference === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
})();
