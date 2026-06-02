const API_BASE = '/api'
const uploadZone = document.getElementById('uploadZone')
const fileInput = document.getElementById('fileInput')
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

let selectedFiles = []
const ALLOWED_EXT = ['.txt', '.pdf', '.epub', '.mobi']

function setLoading(show, title, sub) {
  if (show) {
    loadingTitle.textContent = title || '正在处理'
    loadingSub.textContent = sub || '请稍候…'
    loadingOverlay.classList.add('show')
    loadingOverlay.setAttribute('aria-hidden', 'false')
  } else {
    loadingOverlay.classList.remove('show')
    loadingOverlay.setAttribute('aria-hidden', 'true')
  }
}

function setOverlayText(title, sub) {
  loadingTitle.textContent = title
  loadingSub.textContent = sub
}

uploadZone.addEventListener('click', () => fileInput.click())
uploadZone.addEventListener('dragover', (e) => {
  e.preventDefault()
  uploadZone.classList.add('dragover')
})
uploadZone.addEventListener('dragleave', () => uploadZone.classList.remove('dragover'))
uploadZone.addEventListener('drop', (e) => {
  e.preventDefault()
  uploadZone.classList.remove('dragover')
  addFiles(e.dataTransfer.files)
})

fileInput.addEventListener('change', (e) => addFiles(e.target.files))

function addFiles(files) {
  for (const f of files) {
    const ext = '.' + f.name.split('.').pop().toLowerCase()
    if (ALLOWED_EXT.includes(ext) && !selectedFiles.find(x => x.name === f.name && x.size === f.size)) {
      selectedFiles.push(f)
    }
  }
  renderFileList()
}

function renderFileList() {
  fileList.innerHTML = selectedFiles.map((f, i) => `
    <div class="item">
      <span class="name">${f.name}</span>
      <span class="remove" data-i="${i}">删除</span>
    </div>
  `).join('')
  fileList.querySelectorAll('.remove').forEach(el => {
    el.addEventListener('click', () => {
      selectedFiles.splice(+el.dataset.i, 1)
      renderFileList()
    })
  })
  uploadBtn.disabled = selectedFiles.length === 0
}

clearBtn.addEventListener('click', () => {
  selectedFiles = []
  renderFileList()
  statusCard.classList.remove('show')
  resultPanel.style.display = 'none'
  resultPanel.innerHTML = ''
  hidePathTooltip()
})

uploadBtn.addEventListener('click', async () => {
  if (selectedFiles.length === 0) return
  uploadBtn.disabled = true
  statusCard.classList.remove('show')
  resultPanel.style.display = 'none'
  resultPanel.innerHTML = ''
  hidePathTooltip()
  downloadBtn.style.display = 'none'

  setLoading(true, '正在上传', `共 ${selectedFiles.length} 个文件`)

  const formData = new FormData()
  selectedFiles.forEach(f => formData.append('files', f))

  try {
    const res = await fetch(API_BASE + '/upload', {
      method: 'POST',
      body: formData
    })
    const data = await res.json()
    if (data.error) throw new Error(data.error)
    const taskId = data.task_id

    setOverlayText('正在智能分类', '分析正文并生成中图法分类号，请稍候…')

    const classifyRes = await fetch(API_BASE + '/classify', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ task_id: taskId })
    })
    if (!classifyRes.ok) {
      const errBody = await classifyRes.json().catch(() => ({}))
      throw new Error(errBody.error || '启动分类失败')
    }

    const finalStatus = await new Promise((resolve, reject) => {
      const poll = async () => {
        try {
          const s = await fetch(API_BASE + '/status/' + taskId).then(r => r.json())
          setOverlayText('正在智能分类', s.message || '处理中…')
          if (s.status === 'completed') {
            resolve(s)
            return
          }
          if (s.status === 'failed') {
            reject(new Error(s.message || '分类失败'))
            return
          }
          setTimeout(poll, 1200)
        } catch (e) {
          reject(e)
        }
      }
      poll()
    })

    setLoading(false)
    showResultSuccess(taskId, finalStatus)
  } catch (err) {
    setLoading(false)
    showResultError(err.message || String(err))
  } finally {
    uploadBtn.disabled = false
  }
})

async function showResultSuccess(taskId, statusObj) {
  statusCard.classList.add('show')
  statusText.textContent = '分类完成'
  statusText.className = 'status completed'
  statusMsg.textContent = statusObj.message || '所有文件已归类到对应分类目录中。'

  downloadBtn.href = API_BASE + '/download/' + taskId
  downloadBtn.style.display = 'inline-block'

  try {
    const res = await fetch(API_BASE + '/results/' + taskId)
    const rows = await res.json()
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
  } catch (e) {
    resultPanel.innerHTML = '<div style="color:var(--text-secondary);font-size:13px;padding:8px 0;">结果加载失败</div>'
    resultPanel.style.display = 'block'
  }
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
