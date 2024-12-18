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

async function loadImageSegmenter(model) {
    let module = await import('/third-party/tasks-vision/vision_bundle.mjs');
    let vision = await module.FilesetResolver.forVisionTasks(
        "/third-party/tasks-vision/wasm"
    );

    return await module.ImageSegmenter.createFromOptions(vision, {
            baseOptions: {
                modelAssetPath: model,
            },
            outputCategoryMask: true,
            outputConfidenceMasks: false,
            runningMode: 'VIDEO',
        });
}

onmessage = async e => {
    let data = e.data;
    if(imageSegmenter == null) {
        try {
            imageSegmenter = await loadImageSegmenter(data.model);
            if(imageSegmenter == null)
                throw new Error("loadImageSegmenter returned null");
        } catch(e) {
            postMessage(e);
            return;
        }
        postMessage(null);
        return;
    }

    let bitmap = e.data.bitmap;
    if(!(bitmap instanceof ImageBitmap)) {
        postMessage(new Error('Bad type for worker data'));
        return;
    }

    try {
        let width = bitmap.width;
        let height = bitmap.height;
        imageSegmenter.segmentForVideo(
            bitmap, e.data.timestamp,
            result => {
                /** @type{Uint8Array} */
                let mask = result.categoryMask.getAsUint8Array();
                let id = new ImageData(width, height);
                for(let i = 0; i < mask.length; i++)
                    id.data[4 * i + 3] = mask[i];
                result.close();
                createImageBitmap(id).then(ib => {
                    postMessage({
                        bitmap: bitmap,
                        mask: ib,
                    }, [bitmap, ib]);
                });
            },
        );
    } catch(e) {
        bitmap.close();
        postMessage(e);
    }
};
