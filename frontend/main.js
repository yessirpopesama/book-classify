const API_BASE = '/api'
const uploadZone = document.getElementById('uploadZone')
const fileInput = document.getElementById('fileInput')
const fileList = document.getElementById('fileList')
const clearBtn = document.getElementById('clearBtn')
const uploadBtn = document.getElementById('uploadBtn')
const statusCard = document.getElementById('statusCard')
const statusText = document.getElementById('statusText')
const statusMsg = document.getElementById('statusMsg')
const downloadBtn = document.getElementById('downloadBtn')

let selectedFiles = []
const ALLOWED_EXT = ['.txt', '.pdf', '.epub']

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
})

uploadBtn.addEventListener('click', async () => {
  if (selectedFiles.length === 0) return
  uploadBtn.disabled = true
  statusCard.classList.add('show')
  statusText.textContent = '上传中...'
  statusText.className = 'status'
  statusMsg.textContent = ''
  downloadBtn.style.display = 'none'

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

    statusText.textContent = '正在分类...'
    statusText.className = 'status processing'

    await fetch(API_BASE + '/classify', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ task_id: taskId })
    })

    const checkStatus = async () => {
      const s = await fetch(API_BASE + '/status/' + taskId).then(r => r.json())
      statusMsg.textContent = s.message
      if (s.status === 'completed') {
        statusText.textContent = '分类完成'
        statusText.className = 'status completed'
        downloadBtn.href = API_BASE + '/download/' + taskId
        downloadBtn.style.display = 'inline-block'
        uploadBtn.disabled = false
        return
      }
      if (s.status === 'failed') {
        statusText.textContent = '分类失败'
        statusText.className = 'status failed'
        uploadBtn.disabled = false
        return
      }
      setTimeout(checkStatus, 1500)
    }
    checkStatus()
  } catch (err) {
    statusText.textContent = '出错'
    statusText.className = 'status failed'
    statusMsg.textContent = err.message
    uploadBtn.disabled = false
  }
})
