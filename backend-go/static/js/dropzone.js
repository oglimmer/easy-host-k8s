function initDropzone(zoneId, inputId, isRequired) {
    var zone = document.getElementById(zoneId);
    var input = document.getElementById(inputId);
    var idle = document.getElementById('dropzoneIdle');
    var preview = document.getElementById('dropzonePreview');
    var fileName = document.getElementById('dropzoneFileName');
    var fileMeta = document.getElementById('dropzoneFileMeta');
    var fileIcon = document.getElementById('dropzoneFileIcon');
    var removeBtn = document.getElementById('dropzoneRemove');
    var allowed = ['.html', '.htm', '.zip'];

    function formatSize(bytes) {
        if (bytes < 1024) return bytes + ' B';
        if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
        return (bytes / 1048576).toFixed(1) + ' MB';
    }

    function getExt(name) {
        var i = name.lastIndexOf('.');
        return i > -1 ? name.substring(i).toLowerCase() : '';
    }

    function isZip(name) {
        return getExt(name) === '.zip';
    }

    function showFile(file) {
        if (!file) return;

        var ext = getExt(file.name);
        if (allowed.indexOf(ext) === -1) {
            zone.classList.add('dropzone-error');
            setTimeout(function() { zone.classList.remove('dropzone-error'); }, 800);
            input.value = '';
            return;
        }

        if (file.size > 10 * 1048576) {
            zone.classList.add('dropzone-error');
            setTimeout(function() { zone.classList.remove('dropzone-error'); }, 800);
            input.value = '';
            return;
        }

        fileName.textContent = file.name;
        fileMeta.textContent = formatSize(file.size) + '  \u00b7  ' + ext.substring(1).toUpperCase();

        // Swap icon for zip
        if (isZip(file.name)) {
            fileIcon.innerHTML = '<svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 8v13H3V3h13"/><path d="M16 3v5h5"/><rect x="9" y="10" width="6" height="8" rx="1"/><path d="M12 10v1m0 2v1m0 2v1"/></svg>';
        } else {
            fileIcon.innerHTML = '<svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/><path d="M10 13l2 2 4-4"/></svg>';
        }

        idle.style.display = 'none';
        preview.style.display = 'flex';
        zone.classList.add('dropzone-has-file');
        zone.classList.remove('dropzone-dragover');
    }

    function reset() {
        input.value = '';
        idle.style.display = '';
        preview.style.display = 'none';
        zone.classList.remove('dropzone-has-file');
    }

    // Click to browse
    zone.addEventListener('click', function(e) {
        if (e.target === removeBtn || removeBtn.contains(e.target)) return;
        input.click();
    });

    input.addEventListener('change', function() {
        if (input.files && input.files[0]) {
            showFile(input.files[0]);
        }
    });

    removeBtn.addEventListener('click', function(e) {
        e.stopPropagation();
        reset();
    });

    // Drag events
    var dragCounter = 0;

    zone.addEventListener('dragenter', function(e) {
        e.preventDefault();
        dragCounter++;
        zone.classList.add('dropzone-dragover');
    });

    zone.addEventListener('dragleave', function(e) {
        e.preventDefault();
        dragCounter--;
        if (dragCounter <= 0) {
            dragCounter = 0;
            zone.classList.remove('dropzone-dragover');
        }
    });

    zone.addEventListener('dragover', function(e) {
        e.preventDefault();
    });

    zone.addEventListener('drop', function(e) {
        e.preventDefault();
        dragCounter = 0;
        zone.classList.remove('dropzone-dragover');

        var files = e.dataTransfer.files;
        if (files && files.length > 0) {
            input.files = files;
            showFile(files[0]);
        }
    });
}
