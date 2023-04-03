#!/bin/sh

tmp_filename1="/tmp/extract_translate1"
tmp_filename2="/tmp/extract_translate2"
rm -rf ${tmp_filename1}
rm -rf ${tmp_filename2}

current_dir=$(dirname $(readlink -f "$0"))
cd $current_dir

grep 'translate_text("' static/*.js > ${tmp_filename1}
sed -i 's|^.*translate_text("||g' ${tmp_filename1}
sed -i 's|").*$||g' ${tmp_filename1}

grep 'lang"' static/*.html > ${tmp_filename2}
sed -i 's|.*<input .* value="\([a-zA-Z 0-9:-]*\)".*|\1|g' ${tmp_filename2}
sed -i 's|.*>\([a-zA-Z 0-9:\.\!-\\'"'"']*\)</.*|\1|g' ${tmp_filename2}

cat ${tmp_filename2} >> ${tmp_filename1}
sort --unique -o ${tmp_filename1} ${tmp_filename1}
cat ${tmp_filename1}
