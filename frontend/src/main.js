import * as App from '../wailsjs/go/main/App.js';
import * as runtime from '../wailsjs/runtime/runtime.js';

let isRunning = false;
let totalJobs = 0;
let doneJobs = 0;
let results = [];

// DOM Elements
const btnLoadFile = document.getElementById('btnLoadFile');
const btnStart = document.getElementById('btnStart');
const btnStop = document.getElementById('btnStop');
const btnExport = document.getElementById('btnExport');
const txtDomains = document.getElementById('domains');
const inputWorkers = document.getElementById('workers');
const inputTimeout = document.getElementById('timeout');
const inputRetries = document.getElementById('retries');
const statusText = document.getElementById('statusText');
const progressFill = document.getElementById('progressFill');
const resultsBody = document.getElementById('resultsBody');
const footerText = document.getElementById('footerText');
const resultsTable = document.getElementById('resultsTable');

// Setup event listeners
btnLoadFile.addEventListener('click', async () => {
    try {
        const content = await App.OpenFileDialog();
        if (content) {
            const current = txtDomains.value.trim();
            txtDomains.value = current ? current + '\n' + content : content;
        }
    } catch (e) {
        console.error("Error loading file:", e);
    }
});

btnStart.addEventListener('click', async () => {
    if (isRunning) return;

    const input = txtDomains.value.trim();
    if (!input) {
        alert("Masukkan alamat web terlebih dahulu!");
        return;
    }

    const workers = parseInt(inputWorkers.value) || 10;
    const timeout = parseInt(inputTimeout.value) || 15;
    const retries = parseInt(inputRetries.value) || 3;

    // Reset UI
    resultsBody.innerHTML = '';
    results = [];
    doneJobs = 0;
    totalJobs = 0;
    progressFill.style.width = '0%';
    statusText.innerText = "Memulai...";
    btnStart.disabled = true;
    btnStop.disabled = false;
    btnExport.disabled = true;
    isRunning = true;

    // Call Go Backend
    App.CheckDomains(input, workers, timeout, retries);
});

btnStop.addEventListener('click', () => {
    if (!isRunning) return;
    App.StopCheck();
});

btnExport.addEventListener('click', async () => {
    if (results.length === 0) return;

    let csv = "domain,input_asli,status,error,durasi_ms\n";
    results.forEach(r => {
        csv += `${r.domain},${r.raw},${r.status},${r.error || ''},${r.duration/1000000}\n`;
    });

    try {
        await App.SaveCSVDialog(csv);
    } catch (e) {
        console.error("Error saving CSV:", e);
    }
});

// Setup Wails Events
runtime.EventsOn("onStarted", (total) => {
    totalJobs = total;
    statusText.innerText = `Mengecek ${total} domain...`;
});

runtime.EventsOn("onResult", (result) => {
    results.push(result);
    doneJobs++;
    
    // Update Progress Bar
    const percent = totalJobs > 0 ? (doneJobs / totalJobs) * 100 : 0;
    progressFill.style.width = `${percent}%`;

    // Add to table
    const tr = document.createElement('tr');
    
    // index
    const tdIndex = document.createElement('td');
    tdIndex.innerText = doneJobs;
    tr.appendChild(tdIndex);

    // domain
    const tdDomain = document.createElement('td');
    tdDomain.innerText = result.domain;
    tr.appendChild(tdDomain);

    // status
    const tdStatus = document.createElement('td');
    let statusClass = "status-unknown";
    let statusLabel = "UNKNOWN";
    
    if (result.status === "DIBLOKIR") {
        statusClass = "status-diblokir";
        statusLabel = "DIBLOKIR";
    } else if (result.status === "AMAN") {
        statusClass = "status-aman";
        statusLabel = "AMAN";
    }

    tdStatus.innerText = statusLabel;
    tdStatus.className = statusClass;
    tr.appendChild(tdStatus);

    // keterangan
    const tdDetail = document.createElement('td');
    if (result.error) {
        tdDetail.innerText = result.error;
    } else {
        // duration is in nanoseconds from Go time.Duration
        const ms = Math.round(result.duration / 1000000);
        tdDetail.innerText = `${ms} ms`;
    }
    tr.appendChild(tdDetail);

    resultsBody.appendChild(tr);

    // Auto scroll to bottom
    const container = document.querySelector('.table-container');
    container.scrollTop = container.scrollHeight;

    // Update status text
    statusText.innerText = `Progres: ${doneJobs}/${totalJobs} — ${result.domain}`;
});

runtime.EventsOn("onFinished", (summary) => {
    isRunning = false;
    btnStart.disabled = false;
    btnStop.disabled = true;
    
    if (results.length > 0) {
        btnExport.disabled = false;
    }

    if (summary && summary.Total) {
        // elapsed is in nanoseconds
        const ms = Math.round(summary.Elapsed / 1000000);
        statusText.innerText = `Selesai ${ms}ms — Total: ${summary.Total} | AMAN: ${summary.NotBlocked} | DIBLOKIR: ${summary.Blocked} | UNKNOWN: ${summary.Unknown}`;
    } else {
        statusText.innerText = "Dihentikan.";
    }
});

// Load App info on startup
window.onload = async () => {
    try {
        const version = await App.GetVersion();
        const developer = await App.GetDeveloper();
        footerText.innerText = `Nawala Checker v${version} Crafted by ${developer}`;
    } catch(e) {
        console.error(e);
    }
};
