const API_BASE = '/api'

const TOOLS = {
  classify: {
    id: 'classify',
    title: '图书分类',
    subtitle: '上传书籍，智能分类，一键下载',
    icon: '📚',
    uploadHint: '点击或拖拽书籍、文件夹到此处',
    uploadSub: '自动扫描子目录，仅收录 .txt、.pdf、.epub、.mobi',
    accept: '.txt,.pdf,.epub,.mobi',
    allowFolder: true,
    actionText: '开始上传并分类',
    loadingProcess: '正在智能分类',
    loadingSub: '分析正文并生成中图法分类号，请稍候…',
    uploadUrl: '/upload',
    startUrl: '/classify',
    resultsUrl: (id) => `/results/${id}`,
    downloadName: 'results.zip',
    downloadText: '下载分类结果',
    successTitle: '分类完成',
    successMsg: '所有文件已归类到对应分类目录中。',
    renderResults: renderClassifyResults,
    isAllowed: (name) => {
      const ext = '.' + name.split('.').pop().toLowerCase()
      return ['.txt', '.pdf', '.epub', '.mobi'].includes(ext)
    }
  },
  repair: {
    id: 'repair',
    title: '内容修复',
    subtitle: '优化书名、修复编码与格式、对齐正文并剔除垃圾内容',
    icon: '🔧',
    uploadHint: '点击或拖拽 txt 文件到此处',
    uploadSub: '仅支持 .txt，自动修复 Win/Mac 无法读取的编码与格式问题',
    accept: '.txt',
    allowFolder: true,
    actionText: '开始上传并修复',
    loadingProcess: '正在修复内容',
    loadingSub: '优化文件名、转换编码、对齐正文并剔除广告行…',
    uploadUrl: '/repair/upload',
    startUrl: '/repair',
    resultsUrl: (id) => `/repair/results/${id}`,
    downloadName: 'repaired.zip',
    downloadText: '下载修复结果',
    successTitle: '修复完成',
    successMsg: '所有 txt 已修复，可下载查看修复报告。',
    renderResults: renderRepairResults,
    isAllowed: (name) => name.toLowerCase().endsWith('.txt')
  }
}

let currentTool = 'classify'
let selectedFiles = []

const uploadZone = document.getElementById('uploadZone')
const fileInput = document.getElementById('fileInput')
const folderInput = document.getElementById('folderInput')
const folderLink = document.getElementById('folderLink')
const fileListWrap = document.getElementById('fileListWrap')
const fileCountEl = document.getElementById('fileCount')
const fileList = document.getElementById('fileList')
const clearBtn = document.getElementById('clearBtn')
const uploadBtn = document.getElementById('uploadBtn')
const statusCard = document.getElementById('statusCard')
const statusText = document.getElementById('statusText')
const statusMsg = document.getElementById('statusMsg')
const resultPanel = document.getElementById('resultPanel')
const downloadBtn = document.getElementById('downloadBtn')
const loadingOverlay = document.getElementById('loadingOverlay')
const loadingTitle = document.getElementById('loadingTitle')
const loadingSub = document.getElementById('loadingSub')
const loadingProgressBar = document.getElementById('loadingProgressBar')
const loadingProgressText = document.getElementById('loadingProgressText')
const toolTabs = document.getElementById('toolTabs')
const pageTitle = document.getElementById('pageTitle')
const pageSubtitle = document.getElementById('pageSubtitle')
const uploadHint = document.getElementById('uploadHint')
const uploadSub = document.getElementById('uploadSub')
const uploadIcon = document.getElementById('uploadIcon')

function tool() {
  return TOOLS[currentTool]
}

async function parseJsonResponse(res) {
  const text = await res.text()
  if (!text) return {}
  try {
    return JSON.parse(text)
  } catch {
    if (res.status === 404 && text.includes('page not found')) {
      throw new Error('后端接口不存在，请重启服务：./start.sh')
    }
    const brief = text.length > 120 ? text.slice(0, 120) + '…' : text
    throw new Error(brief || `请求失败 (HTTP ${res.status})`)
  }
}

function setProgress(percent, options = {}) {
  const { indeterminate = false, label } = options
  if (!loadingProgressBar || !loadingProgressText) return

  if (indeterminate) {
    loadingProgressBar.classList.add('indeterminate')
    loadingProgressBar.style.width = ''
    loadingProgressText.textContent = label || '上传中…'
    return
  }

  loadingProgressBar.classList.remove('indeterminate')
  const p = Math.min(100, Math.max(0, Math.round(percent)))
  loadingProgressBar.style.width = p + '%'
  loadingProgressText.textContent = label || (p + '%')
}

function setLoading(show, title, sub) {
  if (show) {
    loadingTitle.textContent = title || '正在处理'
    loadingSub.textContent = sub || '请稍候…'
    loadingOverlay.classList.add('show')
    loadingOverlay.setAttribute('aria-hidden', 'false')
  } else {
    loadingOverlay.classList.remove('show')
    loadingOverlay.setAttribute('aria-hidden', 'true')
    setProgress(0)
  }
}

function setOverlayText(title, sub, progress) {
  loadingTitle.textContent = title
  loadingSub.textContent = sub
  if (progress) {
    if (progress.indeterminate) {
      setProgress(0, { indeterminate: true, label: progress.label })
    } else if (typeof progress.percent === 'number') {
      setProgress(progress.percent, { label: progress.label })
    }
  }
}

function fileKey(f) {
  return (f.webkitRelativePath || f.name) + '\0' + f.size
}

function displayPath(f) {
  const rel = f.webkitRelativePath || f.name
  const idx = rel.lastIndexOf('/')
  if (idx === -1) return { dir: '', name: rel }
  return { dir: rel.slice(0, idx + 1), name: rel.slice(idx + 1) }
}

function addFiles(files) {
  const cfg = tool()
  const existing = new Set(selectedFiles.map(fileKey))
  for (const f of files) {
    const name = f.webkitRelativePath || f.name
    if (!cfg.isAllowed(name)) continue
    const key = fileKey(f)
    if (existing.has(key)) continue
    existing.add(key)
    selectedFiles.push(f)
  }
  renderFileList()
}

function switchTool(next) {
  if (next === currentTool) return
  currentTool = next
  selectedFiles = []
  renderToolUI()
  renderFileList()
  statusCard.classList.remove('show')
  resultPanel.style.display = 'none'
  resultPanel.innerHTML = ''
  downloadBtn.style.display = 'none'
  hidePathTooltip()
}

function renderToolUI() {
  const cfg = tool()
  document.title = `${cfg.title} - 图书工具`
  pageTitle.textContent = cfg.title
  pageSubtitle.textContent = cfg.subtitle
  uploadIcon.textContent = cfg.icon
  uploadHint.textContent = cfg.uploadHint
  uploadSub.textContent = cfg.uploadSub
  uploadBtn.textContent = cfg.actionText
  fileInput.accept = cfg.accept
  folderLink.style.display = cfg.allowFolder ? 'inline-block' : 'none'
  folderInput.style.display = cfg.allowFolder ? '' : 'none'

  toolTabs.querySelectorAll('.tool-tab').forEach(tab => {
    tab.classList.toggle('active', tab.dataset.tool === currentTool)
  })
}

function fileExt(name) {
  const parts = name.split('.')
  return parts.length > 1 ? parts.pop().toLowerCase() : 'other'
}

function fileTypeClass(name) {
  const ext = fileExt(name)
  return ['txt', 'pdf', 'epub', 'mobi'].includes(ext) ? ext : 'other'
}

function fileTypeLabel(name) {
  const ext = fileExt(name)
  return ext === 'other' ? 'FILE' : ext.toUpperCase()
}

function renderFileList() {
  const count = selectedFiles.length
  if (fileCountEl) fileCountEl.textContent = count
  if (fileListWrap) fileListWrap.classList.toggle('has-files', count > 0)

  if (count === 0) {
    fileList.innerHTML = ''
    uploadBtn.disabled = true
    return
  }

  fileList.innerHTML = selectedFiles.map((f, i) => {
    const { dir, name } = displayPath(f)
    const typeClass = fileTypeClass(name)
    const fullPath = dir + name
    return `
    <div class="file-card" title="${esc(fullPath)}">
      <button type="button" class="file-card-remove" data-i="${i}" aria-label="删除">×</button>
      <div class="file-card-icon ${typeClass}">${fileTypeLabel(name)}</div>
      <div class="file-card-name">${esc(name)}</div>
      ${dir ? `<div class="file-card-dir">${esc(dir)}</div>` : ''}
    </div>`
  }).join('')

  fileList.querySelectorAll('.file-card-remove').forEach(el => {
    el.addEventListener('click', (e) => {
      e.stopPropagation()
      selectedFiles.splice(+el.dataset.i, 1)
      renderFileList()
    })
  })
  uploadBtn.disabled = false
}

toolTabs.querySelectorAll('.tool-tab').forEach(tab => {
  tab.addEventListener('click', () => switchTool(tab.dataset.tool))
})

uploadZone.addEventListener('click', (e) => {
  if (e.target.closest('#folderLink')) return
  fileInput.click()
})

folderLink.addEventListener('click', (e) => {
  e.preventDefault()
  e.stopPropagation()
  folderInput.click()
})

uploadZone.addEventListener('dragover', (e) => {
  e.preventDefault()
  uploadZone.classList.add('dragover')
})
uploadZone.addEventListener('dragleave', () => uploadZone.classList.remove('dragover'))
uploadZone.addEventListener('drop', async (e) => {
  e.preventDefault()
  uploadZone.classList.remove('dragover')
  const items = e.dataTransfer?.items
  if (items && items.length > 0) {
    const collected = await collectDroppedFiles(items)
    if (collected.length > 0) {
      addFiles(collected)
      return
    }
  }
  addFiles(e.dataTransfer.files)
})

fileInput.addEventListener('change', (e) => {
  addFiles(e.target.files)
  e.target.value = ''
})

folderInput.addEventListener('change', (e) => {
  addFiles(e.target.files)
  e.target.value = ''
})

async function collectDroppedFiles(items) {
  const files = []
  const tasks = []
  for (const item of items) {
    const entry = item.webkitGetAsEntry?.()
    if (entry) tasks.push(walkEntry(entry, ''))
  }
  const nested = await Promise.all(tasks)
  for (const batch of nested) files.push(...batch)
  return files
}

async function walkEntry(entry, prefix) {
  if (entry.isFile) {
    const file = await new Promise((resolve, reject) => entry.file(resolve, reject))
    file.webkitRelativePath = prefix + entry.name
    return [file]
  }
  if (!entry.isDirectory) return []

  const reader = entry.createReader()
  const children = []
  const readBatch = () => new Promise((resolve, reject) => reader.readEntries(resolve, reject))

  while (true) {
    const batch = await readBatch()
    if (!batch.length) break
    children.push(...batch)
  }

  const nested = await Promise.all(
    children.map(child => walkEntry(child, prefix + entry.name + '/'))
  )
  return nested.flat()
}

clearBtn.addEventListener('click', async () => {
  if (!confirm('确定清空列表并删除本地 data 下的任务数据吗？\n（不会删除中图法索引 data/clc）')) {
    return
  }

  clearBtn.disabled = true
  try {
    const res = await fetch(API_BASE + '/clear', { method: 'POST' })
    const data = await parseJsonResponse(res)
    if (!res.ok || data.error) {
      throw new Error(data.error || '清空失败')
    }

    selectedFiles = []
    renderFileList()
    statusCard.classList.remove('show')
    resultPanel.style.display = 'none'
    resultPanel.innerHTML = ''
    downloadBtn.style.display = 'none'
    hidePathTooltip()
  } catch (err) {
    showResultError(err.message || String(err))
  } finally {
    clearBtn.disabled = false
  }
})

uploadBtn.addEventListener('click', async () => {
  if (selectedFiles.length === 0) return
  const cfg = tool()
  uploadBtn.disabled = true
  statusCard.classList.remove('show')
  resultPanel.style.display = 'none'
  resultPanel.innerHTML = ''
  hidePathTooltip()
  downloadBtn.style.display = 'none'

  setLoading(true, '正在上传', `共 ${selectedFiles.length} 个文件`)
  setProgress(0, { indeterminate: true, label: '上传中…' })

  const formData = new FormData()
  selectedFiles.forEach(f => {
    const uploadName = f.webkitRelativePath || f.name
    formData.append('files', f, uploadName)
  })

  try {
    const res = await fetch(API_BASE + cfg.uploadUrl, {
      method: 'POST',
      body: formData
    })
    const data = await parseJsonResponse(res)
    if (!res.ok || data.error) throw new Error(data.error || '上传失败')
    const taskId = data.task_id
    const total = data.count || selectedFiles.length

    setOverlayText(cfg.loadingProcess, cfg.loadingSub, {
      percent: 0,
      label: total > 0 ? `0 / ${total}` : '0%'
    })

    const startRes = await fetch(API_BASE + cfg.startUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ task_id: taskId })
    })
    if (!startRes.ok) {
      const errBody = await parseJsonResponse(startRes).catch(e => ({ error: e.message }))
      throw new Error(errBody.error || '启动处理失败')
    }

    const finalStatus = await pollStatus(taskId, total, cfg)
    setLoading(false)
    showResultSuccess(taskId, finalStatus, cfg)
  } catch (err) {
    setLoading(false)
    showResultError(err.message || String(err))
  } finally {
    uploadBtn.disabled = false
  }
})

async function pollStatus(taskId, total, cfg) {
  return new Promise((resolve, reject) => {
    const poll = async () => {
      try {
        const statusRes = await fetch(API_BASE + '/status/' + taskId)
        const s = await parseJsonResponse(statusRes)
        const totalFiles = s.total || total
        const done = s.current || 0
        const pct = typeof s.progress === 'number' ? s.progress : (totalFiles > 0 ? Math.round(done * 100 / totalFiles) : 0)
        const progressLabel = totalFiles > 0 ? `${done} / ${totalFiles}` : (pct + '%')
        setOverlayText(cfg.loadingProcess, s.message || '处理中…', { percent: pct, label: progressLabel })
        if (s.status === 'completed') {
          setProgress(100, { label: totalFiles > 0 ? `${totalFiles} / ${totalFiles}` : '100%' })
          resolve(s)
          return
        }
        if (s.status === 'failed') {
          reject(new Error(s.message || '处理失败'))
          return
        }
        setTimeout(poll, 1200)
      } catch (e) {
        reject(e)
      }
    }
    poll()
  })
}

async function showResultSuccess(taskId, statusObj, cfg) {
  statusCard.classList.add('show')
  statusText.textContent = cfg.successTitle
  statusText.className = 'status completed'
  statusMsg.textContent = statusObj.message || cfg.successMsg

  downloadBtn.href = API_BASE + '/download/' + taskId
  downloadBtn.download = cfg.downloadName
  downloadBtn.textContent = cfg.downloadText
  downloadBtn.style.display = 'inline-block'

  try {
    const res = await fetch(API_BASE + cfg.resultsUrl(taskId))
    const rows = await parseJsonResponse(res)
    cfg.renderResults(rows)
  } catch (e) {
    resultPanel.innerHTML = '<div style="color:var(--text-secondary);font-size:13px;padding:8px 0;">结果加载失败</div>'
    resultPanel.style.display = 'block'
  }
}

async function renderClassifyResults(rows) {
  if (!Array.isArray(rows) || rows.length === 0) {
    resultPanel.innerHTML = '<div style="color:var(--text-secondary);font-size:13px;padding:8px 0;">暂无分类数据</div>'
    resultPanel.style.display = 'block'
    return
  }

  const successRows = rows.filter(r => !r.error)
  const errorRows = rows.filter(r => r.error)

  let html = `
    <div class="result-summary">
      <div class="stat">总计 <span class="num">${rows.length}</span> 本</div>
      <div class="stat">成功 <span class="num">${successRows.length}</span></div>
      <div class="stat">未识别 <span class="num">${errorRows.length}</span></div>
    </div>
    <div class="result-table-wrap">
      <table class="result-table">
        <thead>
          <tr>
            <th>#</th>
            <th>书名</th>
            <th>分类号</th>
            <th>最优分类路径</th>
            <th>书籍作者</th>
            <th>书籍国籍</th>
          </tr>
        </thead>
        <tbody>`

  rows.forEach((r, i) => {
    if (r.error) {
      html += `
          <tr>
            <td>${i + 1}</td>
            <td title="${esc(r.book_name)}">${esc(r.book_name)}</td>
            <td class="err-cell" colspan="4">${esc(r.error)}</td>
          </tr>`
    } else {
      html += `
          <tr>
            <td>${i + 1}</td>
            <td title="${esc(r.book_name)}">${esc(r.book_name)}</td>
            <td class="cls-cell">${esc(r.classification)}</td>
            <td class="path-cell" data-path-idx="${i}">
              <span class="path-text">${esc(r.classification_path)}</span>
            </td>
            <td>${esc(r.author)}</td>
            <td>${esc(r.nationality)}</td>
          </tr>`
    }
  })

  html += `
        </tbody>
      </table>
    </div>`

  resultPanel.innerHTML = html
  resultPanel.style.display = 'block'
  bindPathTooltips(rows)
}

async function renderRepairResults(rows) {
  if (!Array.isArray(rows) || rows.length === 0) {
    resultPanel.innerHTML = '<div style="color:var(--text-secondary);font-size:13px;padding:8px 0;">暂无修复数据</div>'
    resultPanel.style.display = 'block'
    return
  }

  const successRows = rows.filter(r => !r.error)
  const errorRows = rows.filter(r => r.error)

  let html = `
    <div class="result-summary">
      <div class="stat">总计 <span class="num">${rows.length}</span> 本</div>
      <div class="stat">成功 <span class="num">${successRows.length}</span></div>
      <div class="stat">失败 <span class="num">${errorRows.length}</span></div>
    </div>`

  rows.forEach((r, i) => {
    const opts = (r.optimizations || []).map(o => `<li>${esc(o)}</li>`).join('')
    const fname = (r.filename_changes || []).map(o => `<li>${esc(o)}</li>`).join('')
    const totalRemoved = r.removed_lines ?? 0
    html += `
    <div class="repair-detail-card" style="background:#fff;border:1px solid var(--border);border-radius:10px;padding:16px;margin-bottom:12px;">
      <div style="font-weight:600;margin-bottom:8px;">${i + 1}. ${esc(r.original_name)}</div>
      <div style="font-size:13px;color:var(--text-secondary);margin-bottom:10px;">
        ${r.error
          ? `<span class="err-cell">${esc(r.error)}</span>`
          : `新文件：<span style="color:var(--text)">${esc(r.new_name)}</span>`}
      </div>
      ${!r.error ? `
      <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:8px;font-size:12px;margin-bottom:12px;">
        <div>编码：${esc(r.encoding)} → ${esc(r.output_encoding || 'utf-8')}</div>
        <div>字数：${r.original_runes ?? '-'} → ${r.final_runes ?? '-'}</div>
        <div>行数：${r.original_lines ?? '-'} → ${r.final_lines ?? '-'}</div>
        <div>大小：${formatBytes(r.original_bytes)} → ${formatBytes(r.final_bytes)}</div>
        <div>剔除：${totalRemoved} 行</div>
      </div>
      ${fname ? `<div style="font-size:12px;margin-bottom:8px;"><strong>文件名优化</strong><ul style="margin:6px 0 0 18px;color:var(--text-secondary);">${fname}</ul></div>` : ''}
      ${opts ? `<div style="font-size:12px;"><strong>优化项</strong><ul style="margin:6px 0 0 18px;color:var(--text-secondary);">${opts}</ul></div>` : ''}
      ` : ''}
    </div>`
  })

  resultPanel.innerHTML = html
  resultPanel.style.display = 'block'
}

function formatBytes(n) {
  if (n == null || n === undefined) return '-'
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  return (n / (1024 * 1024)).toFixed(2) + ' MB'
}

function esc(str) {
  if (!str) return ''
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

let pathTooltipHideTimer = null
let pathTooltipCurrentText = ''

function hidePathTooltip() {
  const layer = document.getElementById('pathTooltipLayer')
  clearTimeout(pathTooltipHideTimer)
  if (layer) {
    layer.classList.remove('show')
    layer.setAttribute('aria-hidden', 'true')
  }
}

function bindPathTooltips(rows) {
  const layer = document.getElementById('pathTooltipLayer')
  const textEl = document.getElementById('pathTooltipText')
  const copyBtn = document.getElementById('pathCopyBtn')
  if (!layer || !textEl || !copyBtn) return

  const hideTooltip = (delay) => {
    clearTimeout(pathTooltipHideTimer)
    pathTooltipHideTimer = setTimeout(hidePathTooltip, delay ? 180 : 0)
  }

  const positionTooltip = (anchor) => {
    const rect = anchor.getBoundingClientRect()
    const gap = 8
    layer.style.left = '0'
    layer.style.top = '0'
    layer.classList.add('show')
    layer.setAttribute('aria-hidden', 'false')

    const tipRect = layer.getBoundingClientRect()
    let left = rect.left
    let top = rect.bottom + gap

    if (left + tipRect.width > window.innerWidth - 16) {
      left = window.innerWidth - tipRect.width - 16
    }
    if (left < 16) left = 16

    if (top + tipRect.height > window.innerHeight - 16) {
      top = rect.top - tipRect.height - gap
    }
    if (top < 16) top = 16

    layer.style.left = `${left}px`
    layer.style.top = `${top}px`
  }

  const showTooltip = (cell, text) => {
    clearTimeout(pathTooltipHideTimer)
    pathTooltipCurrentText = text || ''
    textEl.textContent = pathTooltipCurrentText || '（无分类路径）'
    copyBtn.textContent = '复制'
    copyBtn.classList.remove('copied')
    copyBtn.disabled = !pathTooltipCurrentText
    positionTooltip(cell)
  }

  layer.onmouseenter = () => clearTimeout(pathTooltipHideTimer)
  layer.onmouseleave = () => hideTooltip(false)

  copyBtn.onclick = async (e) => {
    e.preventDefault()
    if (!pathTooltipCurrentText) return
    const ok = await copyText(pathTooltipCurrentText)
    if (ok) {
      copyBtn.textContent = '已复制'
      copyBtn.classList.add('copied')
      setTimeout(() => {
        copyBtn.textContent = '复制'
        copyBtn.classList.remove('copied')
      }, 1500)
    }
  }

  resultPanel.querySelectorAll('.path-cell[data-path-idx]').forEach(cell => {
    const idx = +cell.dataset.pathIdx
    const text = rows[idx]?.classification_path || ''
    cell.onmouseenter = () => showTooltip(cell, text)
    cell.onmouseleave = () => hideTooltip(true)
  })
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  }
}

function showResultError(msg) {
  statusCard.classList.add('show')
  statusText.textContent = '处理失败'
  statusText.className = 'status failed'
  statusMsg.textContent = msg || '未知错误'
  resultPanel.style.display = 'none'
  resultPanel.innerHTML = ''
  downloadBtn.style.display = 'none'
}

renderToolUI()
