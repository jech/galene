// Copyright (c) 2026 by Juliusz Chroboczek.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.  IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.


'use strict';

/**
 * getElementById, then assert that the result is an HTMLButtonElement.
 *
 * @param {string} id
 */
function getButtonElement(id) {
    let elt = document.getElementById(id);
    if(!elt || !(elt instanceof HTMLButtonElement))
        throw new Error(`Couldn't find ${id}`);
    return elt;
}

/**
 * Add an option to an HTMLSelectElement.
 *
 * @param {HTMLSelectElement} select
 * @param {string} label
 * @param {string} [value]
 */
function addSelectOption(select, label, value) {
    if(!value)
        value = label;
    for(let i = 0; i < select.children.length; i++) {
        let child = select.children[i];
        if(!(child instanceof HTMLOptionElement)) {
            console.warn('Unexpected select child');
            continue;
        }
        if(child.value === value) {
            if(child.label !== label) {
                child.label = label;
            }
            return;
        }
    }

    let option = document.createElement('option');
    option.value = value;
    option.textContent = label;
    select.appendChild(option);
}

/**
 * getElementById, then assert that the result is an HTMLSelectElement.
 *
 * @param {string} id
 */
function getSelectElement(id) {
    let elt = document.getElementById(id);
    if(!elt || !(elt instanceof HTMLSelectElement))
        throw new Error(`Couldn't find ${id}`);
    return elt;
}

function getVideoElement() {
    let elt = document.getElementById("video");
    if(!elt || !(elt instanceof HTMLVideoElement))
        throw new Error("Couldn't find video");
    return elt;
}

async function devices() {
    let video = false, audio = false;
    let devices = await navigator.mediaDevices.enumerateDevices();
    devices.forEach(d => {
        if(d.kind === 'videoinput')
            video = true;
        else if(d.kind === 'audioinput')
            audio = true;
    });
    let res = {};
    if(audio)
        res['audio'] = true;
    if(video)
        res['video'] = true;
    return res;
}

/**
 * @param {HTMLSelectElement} select
 * @returns {string}
 */
function selectOptionDefault(select) {
    /* First non-empty option. */
    for(let i = 0; i < select.children.length; i++) {
        let child = select.children[i];
        if(!(child instanceof HTMLOptionElement)) {
            console.warn('Unexpected select child');
            continue;
        }
        if(child.value)
            return child.value;
    }
    /* The empty option is always available. */
    return '';
}

/**
 * Returns true if an HTMLSelectElement has an option with a given value.
 *
 * @param {HTMLSelectElement} select
 * @param {string} value
 */
function selectOptionAvailable(select, value) {
    let children = select.children;
    for(let i = 0; i < children.length; i++) {
        let child = select.children[i];
        if(!(child instanceof HTMLOptionElement)) {
            console.warn('Unexpected select child');
            continue;
        }
        if(child.value === value)
            return true;
    }
    return false;
}

function displayError(message) {
    /** @ts-ignore */
    Toastify({
        text: message,
        duration: 4000,
        close: true,
        position: 'center',
        gravity: 'top',
        className: 'error',
    }).showToast();
}

function reflectCameraSettings() {
    let settings = getSettings();
    let store = false;

    let videoselect = getSelectElement('videoselect');
    if(!settings.hasOwnProperty('video') ||
       !selectOptionAvailable(videoselect, settings.video)) {
        settings.video = selectOptionDefault(videoselect);
        store = true;
    }
    videoselect.value = settings.video;

    let audioselect = getSelectElement('audioselect');
    if(!settings.hasOwnProperty('audio') ||
       !selectOptionAvailable(audioselect, settings.audio)) {
        settings.audio = selectOptionDefault(audioselect);
        store = true;
    }
    audioselect.value = settings.audio;

    if(store)
        storeSettings(settings);
}

async function setMediaChoices() {
    let devices = await navigator.mediaDevices.enumerateDevices();

    let cn = 1, mn = 1;

    devices.forEach(d => {
        let label = d.label;
        if(d.kind === 'videoinput') {
            if(!label)
                label = `Camera ${cn}`;
            addSelectOption(getSelectElement('videoselect'),
                            label, d.deviceId);
            cn++;
        } else if(d.kind === 'audioinput') {
            if(!label)
                label = `Microphone ${mn}`;
            addSelectOption(getSelectElement('audioselect'),
                            label, d.deviceId);
            mn++;
        }
    });
}

/**
 * @param {MediaStream} s
 */
function stopStream(s) {
    s.getTracks().forEach(t => {
        try {
            t.stop();
        } catch(e) {
            console.warn(e);
        }
    });
}

function stopCameraStream() {
    let video = getVideoElement();
    let old = /** @type{MediaStream} */(video.srcObject);
    if(!old)
        return;
    video.srcObject = null;
    stopStream(old);
}

async function setCameraStream(force) {
    let video = getVideoElement();
    let old = /** @type{MediaStream} */(video.srcObject);
    if(!force && !old)
        return;

    let settings = getSettings();
    /** @type{boolean|MediaTrackConstraints} */
    let a = settings.audio ? {deviceId: settings.audio} : false;
    /** @type{boolean|MediaTrackConstraints} */
    let v = settings.video ? {deviceId: settings.video} : false;
    if(v) {
        let resolution = settings.resolution;
        if(resolution) {
            v.width = { ideal: resolution[0] };
            v.height = { ideal: resolution[1] };
        } else if(settings.blackboardMode) {
            v.width = { min: 640, ideal: 1920 };
            v.height = { min: 400, ideal: 1080 };
        } else {
            v.aspectRatio = { ideal: 4/3 };
        }
    }

    let constraints = {audio: a, video: v};
    try {
        let stream = await navigator.mediaDevices.getUserMedia(constraints);
        video.srcObject = stream;
        await video.play();
    } finally {
        if(old)
            stopStream(old);
    }
}

function setButtonsVisibility() {
    let video = getVideoElement();
    if(video.srcObject) {
        getButtonElement('presentbutton').classList.add('invisible');
        getButtonElement('unpresentbutton').classList.remove('invisible');
    } else {
        getButtonElement('presentbutton').classList.remove('invisible');
        getButtonElement('unpresentbutton').classList.add('invisible');
    }
}

/** @type {AnalyserNode} */
let analyser = null;
/** @type {Uint8Array} */
let analyserData = null;
/** @type {number} */
let drawId = null;
/** @type {CanvasRenderingContext2D} */
let ctx = null;
/** @type {AudioContext} */
let audioContext = null;

async function startAnalyser(stream) {
    let canvas = document.getElementById('volume');
    if(!(canvas instanceof HTMLCanvasElement))
        throw new Error('Unexpected type for canvas');
    if(!ctx)
        ctx = canvas.getContext('2d');
    if (!ctx)
        throw new Error("Couldn't get context for canvas");
    if(analyser)
        await stopAnalyser();

    audioContext = new AudioContext();
    analyser = audioContext.createAnalyser();
    analyser.fftSize = 512;
    let source = audioContext.createMediaStreamSource(stream);
    source.connect(analyser);
    if(audioContext.state === "suspended") {
        audioContext.resume();
    }
    analyserData = new Uint8Array(analyser.frequencyBinCount);

    if(!drawId)
        drawId = requestAnimationFrame(drawVolume);
}

async function stopAnalyser() {
    if(!analyser)
        return;
    try {
        analyser.disconnect();
    } finally {
        analyserData = null;
        analyser = null;
        await audioContext.close();
        audioContext = null;
        ctx.clearRect(0, 0, ctx.canvas.width, ctx.canvas.height);
    }
}

async function setAnalyser() {
    let video = getVideoElement();
    if(video.srcObject) {
        await startAnalyser(video.srcObject);
    } else {
        await stopAnalyser();
    }
}

function drawVolume(event) {
    if(!analyser) {
        drawId = null;
        return;
    }

    drawId = requestAnimationFrame(drawVolume);

    analyser.getByteFrequencyData(analyserData);
    let l = analyserData.length;
    let w = ctx.canvas.width;
    let w0 = w / l;
    let h = ctx.canvas.height;

    ctx.clearRect(0, 0, w, h);
    ctx.fillStyle = 'rgb(64, 64, 192)';
    for(let i = 0; i < l; i++) {
        let v = analyserData[i] / 256;
        ctx.fillRect(i * w0, h * (1 - v) , w0, h * v);
    }
}

getSelectElement('videoselect').onchange = async function(e) {
    e.preventDefault();
    if(!(this instanceof HTMLSelectElement))
        throw new Error('Unexpected type for this');
    updateSettings({video: this.value});
    try {
        await setCameraStream();
        await setAnalyser();
    } catch(e) {
        displayError(e);
    } finally {
        setButtonsVisibility();
    }
};

getSelectElement('audioselect').onchange = async function(e) {
    e.preventDefault();
    if(!(this instanceof HTMLSelectElement))
        throw new Error('Unexpected type for this');
    updateSettings({audio: this.value});
    try {
        await setCameraStream();
        await setAnalyser();
    } catch(e) {
        displayError(e);
    } finally {
        setButtonsVisibility();
    }
};

getButtonElement('presentbutton').onclick = async function(e) {
    e.preventDefault();
    let video = getVideoElement();
    if(video.srcObject != null)
        return;
    let button = this;
    if(!(button instanceof HTMLButtonElement))
        throw new Error('Unexpected type for this.');
    button.disabled = true;
    try {
        await setCameraStream(true);
        await setAnalyser();
    } finally {
        button.disabled = false;
        setButtonsVisibility();
    }
};

getButtonElement('unpresentbutton').onclick = async function(e) {
    e.preventDefault();
    let button = this;
    if(!(button instanceof HTMLButtonElement))
        throw new Error('Unexpected type for this.');
    button.disabled = true;
    try {
        stopCameraStream();
        await setAnalyser();
    } finally {
        button.disabled = false;
        setButtonsVisibility();
    }
};

getButtonElement('permissionsbutton').onclick = async function(e) {
    e.preventDefault();
    let ds = await devices();
    if(!('video' in ds || 'audio' in ds)) {
        displayError('No device detected');
        return;
    }
    try {
        let stream = await navigator.mediaDevices.getUserMedia(ds);
        await new Promise((resolve, reject) => setTimeout(resolve, 200));
        stopStream(stream);
    } catch(e) {
        displayError(e);
        return;
    }

    document.getElementById('permissionsdiv').classList.add('invisible');
    document.getElementById('maindiv').classList.remove('invisible');

    try {
        await setMediaChoices();
        reflectCameraSettings();
    } catch(e) {
        console.error(e);
        displayError(e);
        stopCameraStream();
    } finally {
        setButtonsVisibility();
    }
}
