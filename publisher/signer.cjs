// Keep signing payloads (including Cookies) in pipes rather than argv or files.
// All algorithms are imported unchanged from the pinned upstream snapshot.
const fs = require('node:fs');
const path = require('node:path');
const root = path.join(__dirname, 'vendor', 'Spider_XHS', 'xhs_utils');
try {
  const {operation, payload} = JSON.parse(fs.readFileSync(0, 'utf8'));
  let result;
  if (operation === 'sign') result = require(path.join(root, 'xhs_core', 'js', 'sign.js')).signFull(payload);
  else if (operation === 'profile') {
    const profileData = require(path.join(root, 'xhs_creator', 'js', 'profile.js')).generateProfileData(payload);
    result = {profileData};
  } else if (operation === 'b1') result = require(path.join(root, 'xhs_core', 'js', 'b1.js')).generateB1(payload);
  else throw new Error('unsupported operation');
  process.stdout.write(JSON.stringify(result) + '\n');
} catch (_) {
  process.stderr.write('signing_failed\n');
  process.exitCode = 1;
}
