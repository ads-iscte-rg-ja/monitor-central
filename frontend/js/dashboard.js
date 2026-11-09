'use strict';
(() => {
  const host = document.getElementById('host-selector');
  const range = document.getElementById('range-selector');
  const status = document.getElementById('metrics-status');
  let controller;
  const text = (id, value) => { document.getElementById(id).textContent = value; };
  const number = (value) => Number(value).toFixed(1).replace(/\.0$/, '');
  function chart(id, points, max, color) {
    const svg = document.getElementById(id);
    svg.replaceChildren(); svg.setAttribute('viewBox', '0 0 360 180');
    const ns = 'http://www.w3.org/2000/svg';
    for (let i = 0; i < 4; i++) {
      const line = document.createElementNS(ns, 'line');
      line.setAttribute('x1', '32'); line.setAttribute('x2', '352');
      line.setAttribute('y1', String(20 + i * 45)); line.setAttribute('y2', String(20 + i * 45)); svg.append(line);
      const label = document.createElementNS(ns, 'text'); label.setAttribute('x', '0'); label.setAttribute('y', String(24 + i * 45)); label.textContent = number(max * (3 - i) / 3); svg.append(label);
    }
    if (!points.length) return;
    const plot = document.createElementNS(ns, 'polyline');
    plot.setAttribute('fill', 'none'); plot.setAttribute('stroke', color); plot.setAttribute('stroke-width', '2.5');
    plot.setAttribute('points', points.map((v, i) => `${32 + i * 320 / Math.max(points.length - 1, 1)},${155 - Math.min(Number(v) / max, 1) * 135}`).join(' ')); svg.append(plot);
    if (points.length === 1) { const dot = document.createElementNS(ns, 'circle'); dot.setAttribute('cx', '32'); dot.setAttribute('cy', String(155 - Number(points[0]) / max * 135)); dot.setAttribute('r', '4'); dot.setAttribute('fill', color); svg.append(dot); }
  }
  async function refresh() {
    controller?.abort(); controller = new AbortController();
    const signal = controller.signal;
    const params = new URLSearchParams({range: range.value});
    if (host.value) params.set('host_id', host.value);
    try {
      const response = await fetch(`/api/v1/dashboard/metrics?${params}`, {signal});
      if (response.status === 401 || response.redirected) { window.location.assign('/login'); return; }
      if (!response.ok) throw new Error('Metrics unavailable');
      const metrics = await response.json();
      if (signal.aborted) return;
      host.replaceChildren(...metrics.hosts.map((item) => { const option = document.createElement('option'); option.value = String(item.id); option.textContent = `${item.name} (${item.status})`; option.selected = item.id === metrics.selected_host?.id; return option; }));
      host.disabled = !metrics.hosts.length;
      const stats = metrics.stats || [];
      chart('cpu-chart', stats.map((p) => p.cpu_percent), 100, '#818cf8');
      chart('ram-chart', stats.map((p) => p.ram_used_mb / 1024), Math.max(1, ...stats.map((p) => p.ram_total_mb / 1024)), '#4ade80');
      chart('disk-chart', stats.map((p) => p.disk_used_gb), Math.max(1, ...stats.map((p) => p.disk_total_gb)), '#c084fc');
      const latest = metrics.latest;
      text('cpu-current', latest ? `${number(latest.cpu_percent)}%` : 'No samples');
      text('ram-current', latest ? `${number(latest.ram_used_mb / 1024)} / ${number(latest.ram_total_mb / 1024)} GB` : 'No samples');
      text('disk-current', latest ? `${number(latest.disk_used_gb)} / ${number(latest.disk_total_gb)} GB` : 'No samples');
      status.textContent = latest ? `Latest sample: ${new Date(latest.collected_at).toLocaleString()}. History is aggregated; refreshed every 15 seconds.` : 'No samples received for this host.';
      const summaryResponse = await fetch('/api/v1/dashboard/summary', {signal});
      if (summaryResponse.ok) {
        const summary = await summaryResponse.json(); if (signal.aborted) return;
        text('summary-hosts', `${summary.hosts_online} / ${summary.hosts_total}`); text('summary-errors', summary.errors_today); text('summary-critical', `${summary.critical_errors_today} critical`); text('summary-alerts', summary.firing_alerts); text('summary-cpu', `${number(summary.average_cpu)}%`);
        text('system-status', summary.hosts_total > 0 && summary.hosts_online === summary.hosts_total && summary.firing_alerts === 0 ? 'All systems operational' : 'Systems require attention');
      }
    } catch (error) { if (error.name !== 'AbortError') { status.textContent = 'Unable to refresh. Check your connection; retrying in 15 seconds.'; } }
  }
  host.addEventListener('change', refresh); range.addEventListener('change', refresh);
  refresh(); window.setInterval(() => { if (!document.hidden) refresh(); }, 15000);
})();
