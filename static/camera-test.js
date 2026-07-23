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

/**
 * Get a set of constraints for requesting camera and microphone permissions.
 *
 * @returns {Promise<MediaStreamConstraints>}
 */
async function getPermissionsConstraints() {
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
        let child = children[i];
        if(!(child instanceof HTMLOptionElement)) {
            console.warn('Unexpected select child');
            continue;
        }
        if(child.value === value)
            return true;
    }
    return false;
}

function displayError(e) {
    let message = e;
    if(e instanceof Error)
        message = e.message;
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

/**
 * CameraTest encapsulates the camera and video test UI.
 *
 * @param {HTMLVideoElement} video
 * @param {HTMLCanvasElement} canvas
 * @param {HTMLSelectElement} videoselect
 * @param {HTMLSelectElement} audioselect
 *
 * @constructor
 */

function CameraTest(video, canvas, videoselect, audioselect) {
    /** @type {HTMLVideoElement} */
    this.video = video;
    /** @type {HTMLCanvasElement} */
    this.canvas = canvas;
    /** @type {HTMLSelectElement}  */
    this.videoselect = videoselect;
    /** @type {HTMLSelectElement}  */
    this.audioselect = audioselect;
    /** @type {AnalyserNode} */
    this.analyser = null;
    /** @type {Uint8Array} */
    this.analyserData = null;
    /** @type {number} */
    this.drawId = null;
    /** @type {CanvasRenderingContext2D} */
    this.ctx = null;
    /** @type {AudioContext} */
    this.audioContext = null;
    /** @type {boolean} */
    this.permissionsRequested = false;
}

CameraTest.prototype.reflectSettings = function() {
    let settings = getSettings();
    let store = false;

    if(!settings.hasOwnProperty('video') ||
       !selectOptionAvailable(this.videoselect, settings.video)) {
        settings.video = selectOptionDefault(this.videoselect);
        store = true;
    }
    this.videoselect.value = settings.video;

    if(!settings.hasOwnProperty('audio') ||
       !selectOptionAvailable(this.audioselect, settings.audio)) {
        settings.audio = selectOptionDefault(this.audioselect);
        store = true;
    }
    this.audioselect.value = settings.audio;

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
            addSelectOption(getSelectElement('test-videoselect'),
                            label, d.deviceId);
            cn++;
        } else if(d.kind === 'audioinput') {
            if(!label)
                label = `Microphone ${mn}`;
            addSelectOption(getSelectElement('test-audioselect'),
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

CameraTest.prototype.stopStream = async function() {
    let old = /** @type{MediaStream} */(this.video.srcObject);
    if(!old)
        return;
    this.video.srcObject = null;
    stopStream(old);
}

CameraTest.prototype.setStream = async function(force) {
    let old = /** @type{MediaStream} */(this.video.srcObject);
    if(!force && !old)
        return;

    this.video.srcObject = null; // in case getUserMedia throws

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
        this.video.srcObject = stream;
        await this.video.play();
    } finally {
        if(old)
            stopStream(old);
    }
}

CameraTest.prototype.startAnalyser = async function() {
    if(!this.ctx)
        this.ctx = this.canvas.getContext('2d');
    if (!this.ctx)
        throw new Error("Couldn't get context for canvas");
    if(this.analyser)
        await this.stopAnalyser();

    this.audioContext = new AudioContext();
    this.analyser = this.audioContext.createAnalyser();
    this.analyser.fftSize = 512;
    let src = this.video.srcObject;
    if(!(src instanceof MediaStream))
        throw new Error('Unexpected type for srcObject');
    let source = this.audioContext.createMediaStreamSource(src);
    source.connect(this.analyser);
    if(this.audioContext.state === "suspended") {
        this.audioContext.resume();
    }
    this.analyserData = new Uint8Array(this.analyser.frequencyBinCount);

    if(!this.drawId)
        this.drawId = requestAnimationFrame(e => this.drawFFT());
}

CameraTest.prototype.stopAnalyser = async function() {
    if(!this.analyser)
        return;
    try {
        this.analyser.disconnect();
    } finally {
        this.analyserData = null;
        this.analyser = null;
        await this.audioContext.close();
        this.audioContext = null;
        this.ctx.clearRect(0, 0, this.canvas.width, this.canvas.height);
    }
}

CameraTest.prototype.setAnalyser = async function() {
    if(this.video.srcObject) {
        await this.startAnalyser();
    } else {
        await this.stopAnalyser();
    }
}

CameraTest.prototype.drawFFT = function() {
    if(!this.analyser) {
        this.drawId = null;
        return;
    }

    this.drawId = requestAnimationFrame(e => this.drawFFT());

    function drawText(ctx, text, x, y, position) {
        let metrics = ctx.measureText(text);
        let xx;
        switch(position) {
        case 'left':
            xx = x;
            break;
        case 'centered':
            xx = x - metrics.width / 2;
            break;
        case 'right':
            xx = x - metrics.width;
            break;
        default:
            throw new Error('Bad value for position');
        }
        ctx.fillText(text, xx, y);
    }

    this.analyser.getByteFrequencyData(this.analyserData);
    let l = this.analyserData.length;
    let w = this.ctx.canvas.width;
    let w0 = w / l;
    let h = this.ctx.canvas.height;
    const lineSize = 10;
    let h0 = h - lineSize;

    this.ctx.clearRect(0, 0, w, h);
    this.ctx.fillStyle = 'rgb(96, 96, 96)';
    this.ctx.font = '9px sans-serif';
    drawText(this.ctx, `0\u2009kHz`, 0, h - 2, 'left');
    drawText(this.ctx, `${this.audioContext.sampleRate / 4000}\u2009kHz`,
             w / 2, h - 2, 'centered');
    drawText(this.ctx,`${this.audioContext.sampleRate / 2000}\u2009kHz`,
             w, h - 2, 'right');
    this.ctx.fillStyle = 'rgb(64, 64, 192)';
    for(let i = 0; i < l; i++) {
        let v = this.analyserData[i] / 256;
        this.ctx.fillRect(i * w0, h0 * (1 - v) , w0, h0 * v);
    }
}

CameraTest.prototype.requestPermissions = async function() {
    if(this.permissionsRequested)
        return false;

    let ds = await getPermissionsConstraints();
    if(!('video' in ds || 'audio' in ds))
        throw new Error('No device detected');
    let stream = await navigator.mediaDevices.getUserMedia(ds);
    await new Promise((resolve, reject) => setTimeout(resolve, 200));
    stopStream(stream);
    this.permissionsRequested = true;
    return true;
}

/** @type{CameraTest} */
let cameraTest = null;

document.getElementById('test-camera').ontoggle = async function(e) {
    let details = this;
    if(!(details instanceof HTMLDetailsElement))
        throw new Error('Unexpected type for this');
    if(details.open) {
        if(!cameraTest) {
            let video = document.getElementById('test-video');
            if(!(video instanceof HTMLVideoElement))
               throw new Error('Bad type for video');
            let canvas = document.getElementById('test-fft');
            if(!(canvas instanceof HTMLCanvasElement))
               throw new Error('Bad type for FFT canvas');
            cameraTest = new CameraTest(
                video, canvas,
                getSelectElement('test-videoselect'),
                getSelectElement('test-audioselect'),
            );
        }
        try {
            if(await cameraTest.requestPermissions()) {
                await setMediaChoices();
                cameraTest.reflectSettings();
            }
            if(cameraTest.video.srcObject != null)
                return;
            await cameraTest.setStream(true);
            await cameraTest.setAnalyser();
        } catch(e) {
            displayError(e);
            await cameraTest.stopStream();
            await cameraTest.setAnalyser();
        }
    } else {
        try {
            await cameraTest.stopStream();
            await cameraTest.setAnalyser();
        } catch(e) {
            displayError(e);
        }
    }
}

getSelectElement('test-videoselect').onchange = async function(e) {
    e.preventDefault();
    if(!(this instanceof HTMLSelectElement))
        throw new Error('Unexpected type for this');
    if(!cameraTest)
        throw new Error('Camera test not started');
    updateSettings({video: this.value});
    try {
        await cameraTest.setStream();
        await cameraTest.setAnalyser();
    } catch(e) {
        displayError(e);
    }
};

getSelectElement('test-audioselect').onchange = async function(e) {
    e.preventDefault();
    if(!(this instanceof HTMLSelectElement))
        throw new Error('Unexpected type for this');
    if(!cameraTest)
        throw new Error('Camera test not started');
    updateSettings({audio: this.value});
    try {
        await cameraTest.setStream();
        await cameraTest.setAnalyser();
    } catch(e) {
        displayError(e);
    }
};
