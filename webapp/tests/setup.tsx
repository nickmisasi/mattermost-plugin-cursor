// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import '@testing-library/jest-dom';

// jsdom has no EventSource. Components guard on its absence, but tests that do
// exercise streaming can replace this stub.
if (typeof (global as {EventSource?: unknown}).EventSource === 'undefined') {
    (global as {EventSource?: unknown}).EventSource = undefined;
}

export {};
