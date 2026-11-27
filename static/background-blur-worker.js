// Copyright (c) 2024 by Juliusz Chroboczek.

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

let imageSegmenter;

/** @type{Float32Array} */
let previousMask = null;

async function loadImageSegmenter(model) {
    let module = await import('/third-party/tasks-vision/vision_bundle.mjs');
    let vision = await module.FilesetResolver.forVisionTasks(
        "/third-party/tasks-vision/wasm"
    );

    return await module.ImageSegmenter.createFromOptions(vision, {
            baseOptions: {
                modelAssetPath: model,
            },
            outputCategoryMask: false,
            outputConfidenceMasks: true,
            runningMode: 'VIDEO',
        });
}

async function foregroundMask(bitmap, timestamp) {
    if(!(bitmap instanceof ImageBitmap))
        throw new Error('Bad type for worker data');

    try {
        let width = bitmap.width;
        let height = bitmap.height;
        let p = new Promise((resolve, reject) =>
            imageSegmenter.segmentForVideo(
                bitmap, timestamp,
                result => resolve(result),
            ));
        let result = await p;
        let mask = result.confidenceMasks[0].getAsFloat32Array();
        if(!previousMask || previousMask.length !== mask.length)
            previousMask = new Float32Array(mask);
        let id = new ImageData(width, height);
        for(let i = 0; i < mask.length; i++) {
            let m = (previousMask[i] + mask[i]) / 2;
            previousMask[i] = m;
            id.data[4 * i + 3] = 255 - m * 255;
        }
        result.close();

        let ib = await createImageBitmap(id);
        return {
            bitmap: bitmap,
            mask: ib,
        };
    } catch(e) {
        bitmap.close();
        throw(e);
    }
}

onmessage = async e => {
    try {
        let data = e.data;
        if(data.model) {
            if(imageSegmenter)
                throw new Error("image segmenter already initialised");
            imageSegmenter = await loadImageSegmenter(data.model);
            if(!imageSegmenter)
                throw new Error("loadImageSegmenter returned null");
            postMessage(null);
        } else if(data.bitmap) {
            if(imageSegmenter == null)
                throw new Error("image segmenter not initialised");
            let mask = await foregroundMask(data.bitmap, data.timestamp);
            postMessage(mask, [mask.bitmap, mask.mask]);
        } else {
            throw new Error("unexpected message type");
        }
    } catch(e) {
        postMessage(e);
    }
}
